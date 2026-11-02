package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/controller"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/gateway"
	"github.com/raefon/rehydrator/internal/health"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/rclone"
	"github.com/raefon/rehydrator/internal/seerr"
	"github.com/raefon/rehydrator/internal/syncer"
	"github.com/raefon/rehydrator/internal/torbox"
	"github.com/raefon/rehydrator/internal/webui"
	"net/http"
)

// version is overridden by the release build with -ldflags; the default keeps
// local development builds identifiable as well.
var version = "0.8.1"

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	role := flag.String("role", "", "runtime role: combined, gateway or controller (overrides config/env)")
	showVersion := flag.Bool("version", false, "print Rehydrator version and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *showVersion {
		fmt.Printf("Rehydrator %s\n", version)
		return
	}
	slog.Info("rehydrator version", "version", version)

	cfg, err := config.LoadWithRole(*configPath, *role)
	if err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	if cfg.ConfigCreated {
		slog.Info("default config created", "path", cfg.ConfigPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	repo, err := db.New(ctx, cfg.PostgresURL)
	if err != nil {
		slog.Error("postgres connection failed", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	if cfg.DBAutoMigrate {
		if err := repo.InitSchema(ctx); err != nil {
			slog.Error("database auto-migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("database schema initialized")
	}

	if cfg.Role == config.RoleGateway {
		runGateway(ctx, cfg, repo)
		return
	}

	radarrClient := arr.NewClient("radarr", cfg.RadarrURL, cfg.RadarrAPIKey)
	sonarrClient := arr.NewClient("sonarr", cfg.SonarrURL, cfg.SonarrAPIKey)
	seerrClient := seerr.NewClient(cfg.SeerrURL, cfg.SeerrAPIKey)
	var rcloneClient *rclone.Client
	if cfg.RcloneRCEnabled {
		rcloneClient = rclone.NewClient(cfg.RcloneRCURL, cfg.RcloneRCUsername, cfg.RcloneRCPassword, cfg.CSIPath, cfg.RcloneRCTimeout)
	}
	var jellyfinClient *jellyfin.Client
	if cfg.Jellyfin.Enabled {
		jellyfinClient = jellyfin.NewClient(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey, time.Duration(cfg.Jellyfin.TimeoutSeconds)*time.Second, cfg.Jellyfin.RefreshMode)
	}

	var cloud *gateway.Service
	var gatewayQB, gatewaySAB, gatewayDAV, gatewayDashboard http.HandlerFunc
	var gatewayUI http.Handler
	if cfg.Gateway.Enabled {
		gatewayConfig := cfg.Gateway
		gatewayConfig.RecoverMissing = gatewayConfig.RecoverMissing && cfg.RearmEnabled
		if cfg.Role == config.RoleController {
			cloud = gateway.New(gatewayConfig, cfg.Tenant, repo, nil, nil, cfg.Gateway.SonarrCategory, cfg.Gateway.RadarrCategory)
			cloud.SetRole(config.RoleController)
			go cloud.RunMountWorker(ctx)
		} else {
			cloud = gateway.New(gatewayConfig, cfg.Tenant, repo, torbox.NewTorrentClient(cfg.Gateway.APIKey), torbox.NewUsenetClient(cfg.Gateway.APIKey), cfg.Gateway.SonarrCategory, cfg.Gateway.RadarrCategory)
			gatewayQB = cloud.QBittorrent
			gatewaySAB = cloud.SAB
			gatewayDAV = cloud.DAV
			gatewayDashboard = cloud.Dashboard
			var err error
			gatewayUI, err = cloud.ConfigureDashboard(ctx, cfg.Gateway.OIDC, webui.Handler())
			if err != nil {
				slog.Error("dashboard OIDC initialization failed", "error", err)
				os.Exit(1)
			}
			go cloud.Run(ctx)
		}
	}

	ctrl := controller.New(controller.Options{
		Gateway:                    cloud,
		FileHealth:                 cfg.FileHealth,
		Tenant:                     cfg.Tenant,
		Repo:                       repo,
		Radarr:                     radarrClient,
		Sonarr:                     sonarrClient,
		CSI:                        csi.NewChecker(cfg.CSIPath),
		Rclone:                     rcloneClient,
		Jellyfin:                   jellyfinClient,
		JellyfinConfig:             cfg.Jellyfin,
		RadarrCategory:             cfg.Gateway.RadarrCategory,
		SonarrCategory:             cfg.Gateway.SonarrCategory,
		RearmEnabled:               cfg.RearmEnabled,
		MaxRearmsPerRun:            cfg.MaxRearmsPerRun,
		RcloneRefreshAfterRearm:    cfg.RcloneRCRefreshAfterRearm,
		SelfHealEnabled:            cfg.SelfHealEnabled,
		SelfHealInterval:           cfg.SelfHealInterval,
		SelfHealRefreshAvailable:   cfg.SelfHealRefreshAvailable,
		SelfHealRecentHours:        cfg.SelfHealRecentHours,
		SelfHealMaxRefreshesPerRun: cfg.SelfHealMaxRefreshesPerRun,
		Interval:                   cfg.ReconcileInterval,
		CSIVisibilityTimeout:       cfg.CSIVisibilityTimeout,
		CSIVisibilityPoll:          cfg.CSIVisibilityPoll,
		CSIVisibilityRetry:         cfg.CSIVisibilityRetry,
		CacheGrace:                 cfg.CacheGrace,
		MaxRetries:                 cfg.MaxRetries,
		ConcurrentWorkers:          cfg.ConcurrentWorkers,
	})

	slog.Info("rehydrator starting",
		"config", cfg.ConfigPath,
		"tenant", cfg.Tenant,
		"role", cfg.Role,
		"interval", cfg.ReconcileInterval.String(),
		"cache_grace", cfg.CacheGrace.String(),
		"csi_path", cfg.CSIPath,
		"radarr_category", cfg.Gateway.RadarrCategory,
		"sonarr_category", cfg.Gateway.SonarrCategory,
		"rearm_backend", "torbox_gateway",
		"rearm_enabled", cfg.RearmEnabled,
		"max_rearms_per_run", cfg.MaxRearmsPerRun,
		"csi_visibility_timeout", cfg.CSIVisibilityTimeout.String(),
		"csi_visibility_poll", cfg.CSIVisibilityPoll.String(),
		"csi_visibility_retry", cfg.CSIVisibilityRetry.String(),
		"rclone_rc_enabled", cfg.RcloneRCEnabled,
		"rclone_rc_refresh_after_rearm", cfg.RcloneRCRefreshAfterRearm,
		"jellyfin_enabled", cfg.Jellyfin.Enabled,
		"jellyfin_url", cfg.Jellyfin.URL,
		"self_heal_enabled", cfg.SelfHealEnabled,
		"self_heal_interval", cfg.SelfHealInterval.String(),
		"self_heal_refresh_available", cfg.SelfHealRefreshAvailable,
		"self_heal_recent_hours", cfg.SelfHealRecentHours,
		"self_heal_max_refreshes_per_run", cfg.SelfHealMaxRefreshesPerRun,
		"health_addr", cfg.HealthAddr,
		"api_enabled", cfg.APIEnabled,
		"api_require_token", cfg.APIRequireToken,
		"metrics_enabled", cfg.MetricsEnabled,
		"playback_enabled", cfg.PlaybackEnabled,
		"playback_rearm_on_play", cfg.PlaybackRearmOnPlay,
		"playback_cooldown", cfg.PlaybackCooldown.String(),
		"playback_ignored_titles", cfg.PlaybackIgnoredTitles,
		"playback_ignored_title_contains", cfg.PlaybackIgnoredTitleContains,
		"radarr_sync_enabled", cfg.RadarrSyncEnabled,
		"radarr_sync_interval", cfg.RadarrSyncInterval.String(),
		"sonarr_sync_enabled", cfg.SonarrSyncEnabled,
		"sonarr_sync_interval", cfg.SonarrSyncInterval.String(),
		"seerr_url", cfg.SeerrURL,
		"seerr_sync_enabled", cfg.SeerrSyncEnabled,
		"seerr_sync_interval", cfg.SeerrSyncInterval.String(),
		"seerr_sync_limit", cfg.SeerrSyncLimit,
		"workers", cfg.ConcurrentWorkers,
	)

	var sourceLookup func(context.Context, model.MediaCacheState) (model.TorrentMetadata, error)
	if cfg.Gateway.Enabled {
		sourceLookup = cloud.SourceForImport
	}
	var radarrSyncer *syncer.RadarrSyncer
	var sonarrSyncer *syncer.SonarrSyncer
	var seerrSyncer *syncer.SeerrSyncer

	if cfg.RadarrSyncEnabled {
		radarrSyncer = syncer.NewRadarr(syncer.RadarrOptions{
			SourceLookup: sourceLookup,
			Repo:         repo,
			Radarr:       radarrClient,
			Tenant:       cfg.Tenant,
			Category:     cfg.Gateway.RadarrCategory,
			Interval:     cfg.RadarrSyncInterval,
			CacheGrace:   cfg.CacheGrace,
			OnImported: func(syncCtx context.Context, item model.MediaCacheState, reason string) {
				ctrl.ScheduleInitialImportJellyfinRefresh(ctx, item, "radarr_"+reason)
			},
		})
	}

	if cfg.SonarrSyncEnabled {
		sonarrSyncer = syncer.NewSonarr(syncer.SonarrOptions{
			SourceLookup: sourceLookup,
			Repo:         repo,
			Sonarr:       sonarrClient,
			Tenant:       cfg.Tenant,
			Category:     cfg.Gateway.SonarrCategory,
			Interval:     cfg.SonarrSyncInterval,
			CacheGrace:   cfg.CacheGrace,
			OnImported: func(syncCtx context.Context, item model.MediaCacheState, reason string) {
				ctrl.ScheduleInitialImportJellyfinRefresh(ctx, item, "sonarr_"+reason)
			},
		})
	}

	if cfg.SeerrSyncEnabled {
		seerrSyncer = syncer.NewSeerr(syncer.SeerrOptions{
			Repo:     repo,
			Seerr:    seerrClient,
			Tenant:   cfg.Tenant,
			Interval: cfg.SeerrSyncInterval,
			Limit:    cfg.SeerrSyncLimit,
		})
	}

	var refreshRadarr func(context.Context) error
	if radarrSyncer != nil {
		refreshRadarr = radarrSyncer.SyncOnce
	}
	var refreshSonarr func(context.Context) error
	if sonarrSyncer != nil {
		refreshSonarr = func(ctx context.Context) error { return sonarrSyncer.SyncOnceSource(ctx, "api_or_webhook") }
	}
	var refreshSeerr func(context.Context) error
	if seerrSyncer != nil {
		refreshSeerr = seerrSyncer.SyncOnce
	}

	var jellyfinItemPath func(context.Context, string, string) (string, error)
	if jellyfinClient != nil {
		jellyfinItemPath = jellyfinClient.ItemPath
	}

	var jellyfinRefreshMovie, jellyfinRefreshSeries func(context.Context, int) error
	var jellyfinRefreshLibrary func(context.Context) error
	if jellyfinClient != nil {
		refreshItem := func(mt model.MediaType) func(context.Context, int) error {
			return func(ctx context.Context, id int) error {
				item, found, err := repo.GetState(ctx, cfg.Tenant, mt, id)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("tracked media not found")
				}
				err = jellyfinClient.RefreshPath(ctx, item.SymlinkPath)
				status, message := "success", ""
				if err != nil {
					status = "failed"
					message = err.Error()
				}
				_ = repo.RecordJellyfinRefresh(ctx, cfg.Tenant, item.ID, item.ArrID, "manual_refresh", string(mt)+"_file", item.SymlinkPath, status, message)
				return err
			}
		}
		jellyfinRefreshMovie = refreshItem(model.MediaMovie)
		jellyfinRefreshSeries = refreshItem(model.MediaSeries)
		jellyfinRefreshLibrary = func(ctx context.Context) error {
			err := jellyfinClient.RefreshLibrary(ctx)
			status, message := "success", ""
			if err != nil {
				status = "failed"
				message = err.Error()
			}
			_ = repo.RecordJellyfinRefresh(ctx, cfg.Tenant, "", 0, "manual_library_refresh", "library", "", status, message)
			return err
		}
	}

	dependencyChecks := map[string]func(context.Context) error{
		"postgres": repo.Ping,
	}
	if radarrClient != nil && radarrClient.Configured() {
		dependencyChecks["radarr"] = radarrClient.Ping
	}
	if sonarrClient != nil && sonarrClient.Configured() {
		dependencyChecks["sonarr"] = sonarrClient.Ping
	}

	if seerrClient != nil && seerrClient.Configured() {
		dependencyChecks["seerr"] = func(ctx context.Context) error { _, err := seerrClient.Requests(ctx, 1); return err }
	}

	if jellyfinClient != nil {
		dependencyChecks["jellyfin"] = jellyfinClient.Ping
	}

	var healthServer *health.Server
	if cfg.APIEnabled {
		healthServer = health.NewAPIServer(health.APIOptions{
			GatewayQB: gatewayQB, GatewaySAB: gatewaySAB, GatewayDAV: gatewayDAV, GatewayDashboard: gatewayDashboard, GatewayUI: gatewayUI,
			Readiness: func(ctx context.Context) error {
				if cfg.Gateway.Enabled {
					return repo.GatewayCatalogReady(ctx, cfg.Tenant)
				}
				return repo.Ping(ctx)
			},
			CaptureGrab:                  ctrl.CaptureGrab,
			RecordGrabImport:             ctrl.RecordGrabImport,
			Addr:                         cfg.HealthAddr,
			Repo:                         repo,
			Tenant:                       cfg.Tenant,
			Token:                        cfg.APIToken,
			RequireToken:                 cfg.APIRequireToken,
			MetricsEnabled:               cfg.MetricsEnabled,
			PlaybackEnabled:              cfg.PlaybackEnabled,
			JellyfinItemPath:             jellyfinItemPath,
			JellyfinRefreshMovie:         jellyfinRefreshMovie,
			JellyfinRefreshSeries:        jellyfinRefreshSeries,
			JellyfinRefreshLibrary:       jellyfinRefreshLibrary,
			PlaybackRearmOnPlay:          cfg.PlaybackRearmOnPlay,
			PlaybackCooldown:             cfg.PlaybackCooldown,
			PlaybackIgnoredTitles:        cfg.PlaybackIgnoredTitles,
			PlaybackIgnoredTitleContains: cfg.PlaybackIgnoredTitleContains,
			RefreshRadarr:                refreshRadarr,
			RefreshSonarr:                refreshSonarr,
			RefreshSeerr:                 refreshSeerr,
			SelfHealRun: func(runCtx context.Context) error {
				ctrl.RunSelfHealOnce(ctx)
				return nil
			},
			DependencyChecks: dependencyChecks,
		})
	} else {
		healthServer = health.NewServer(cfg.HealthAddr)
	}
	go healthServer.Run(ctx)

	if radarrSyncer != nil {
		go radarrSyncer.Run(ctx)
	}
	if sonarrSyncer != nil {
		go sonarrSyncer.Run(ctx)
	}
	if seerrSyncer != nil {
		go seerrSyncer.Run(ctx)
	}

	if err := ctrl.Run(ctx); err != nil {
		slog.Error("controller stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("rehydrator stopped")
}

// Gateway startup deliberately has no Arr clients, CSI checker, filesystem
// staging, permission-marker wait or dependency on a tenant controller.
func runGateway(ctx context.Context, cfg config.Config, repo *db.Repo) {
	gatewayConfig := cfg.Gateway
	gatewayConfig.RecoverMissing = gatewayConfig.RecoverMissing && cfg.RearmEnabled
	service := gateway.New(gatewayConfig, cfg.Tenant, repo, torbox.NewTorrentClient(cfg.Gateway.APIKey), torbox.NewUsenetClient(cfg.Gateway.APIKey), cfg.Gateway.SonarrCategory, cfg.Gateway.RadarrCategory)
	service.SetRole(config.RoleGateway)
	ui, err := service.ConfigureDashboard(ctx, cfg.Gateway.OIDC, webui.Handler())
	if err != nil {
		slog.Error("dashboard OIDC initialization failed", "error", err)
		os.Exit(1)
	}
	server := health.NewAPIServer(health.APIOptions{Addr: cfg.HealthAddr, GatewayQB: service.QBittorrent, GatewaySAB: service.SAB, GatewayDAV: service.DAV, GatewayDashboard: service.Dashboard, GatewayUI: ui, Readiness: func(ctx context.Context) error { return repo.GatewayCatalogReady(ctx, cfg.Tenant) }})
	slog.Info("standalone gateway starting", "tenant", cfg.Tenant, "role", cfg.Role, "addr", cfg.HealthAddr)
	go service.Run(ctx)
	server.Run(ctx)
}
