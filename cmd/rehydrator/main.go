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
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/health"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/plex"
	"github.com/raefon/rehydrator/internal/rclone"
	"github.com/raefon/rehydrator/internal/seerr"
	"github.com/raefon/rehydrator/internal/syncer"
	"github.com/raefon/rehydrator/internal/torbox"
)

// version is overridden by the release build with -ldflags; the default keeps
// local development builds identifiable as well.
var version = "0.5.2"

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	showVersion := flag.Bool("version", false, "print Rehydrator version and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *showVersion {
		fmt.Printf("Rehydrator %s\n", version)
		return
	}
	slog.Info("rehydrator version", "version", version)

	cfg, err := config.Load(*configPath)
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

	radarrClient := arr.NewClient("radarr", cfg.RadarrURL, cfg.RadarrAPIKey)
	sonarrClient := arr.NewClient("sonarr", cfg.SonarrURL, cfg.SonarrAPIKey)
	decypharrClient := decypharr.NewClient(cfg.DecypharrURL, cfg.DecypharrUsername, cfg.DecypharrPassword)
	torboxClient := torbox.NewClient(cfg.TorBoxAPIKey)
	seerrClient := seerr.NewClient(cfg.SeerrURL, cfg.SeerrAPIKey)
	var rcloneClient *rclone.Client
	if cfg.RcloneRCEnabled {
		rcloneClient = rclone.NewClient(cfg.RcloneRCURL, cfg.RcloneRCUsername, cfg.RcloneRCPassword, cfg.CSIPath, cfg.RcloneRCTimeout)
	}
	var plexClient *plex.Client
	if cfg.PlexEnabled {
		plexClient = plex.NewClient(cfg.PlexURL, cfg.PlexToken, cfg.PlexMovieSectionID, cfg.PlexTVSectionID, time.Duration(cfg.PlexRefreshTimeoutSeconds)*time.Second)
	}

	ctrl := controller.New(controller.Options{
		Tenant:                         cfg.Tenant,
		Repo:                           repo,
		Radarr:                         radarrClient,
		Sonarr:                         sonarrClient,
		Decypharr:                      decypharrClient,
		TorBox:                         torboxClient,
		CSI:                            csi.NewChecker(cfg.CSIPath),
		Rclone:                         rcloneClient,
		Plex:                           plexClient,
		RadarrCategory:                 cfg.DecypharrRadarrCategory,
		SonarrCategory:                 cfg.DecypharrSonarrCategory,
		DeleteFilesOnPrune:             cfg.DecypharrDeleteFilesOnPrune,
		PruneEnabled:                   cfg.PruneEnabled,
		RearmEnabled:                   cfg.RearmEnabled,
		MaxPrunesPerRun:                cfg.MaxPrunesPerRun,
		MaxRearmsPerRun:                cfg.MaxRearmsPerRun,
		PruneWaitForCSIGone:            cfg.PruneWaitForCSIGone,
		RearmShortCircuitIfVisible:     cfg.RearmShortCircuitIfCSIVisible,
		RcloneRefreshAfterRearm:        cfg.RcloneRCRefreshAfterRearm,
		PlexEnabled:                    cfg.PlexEnabled,
		PlexRefreshAfterRearm:          cfg.PlexRefreshAfterRearm,
		PlexRefreshAfterVisibility:     cfg.PlexRefreshAfterVisibility,
		PlexRefreshAfterImport:         cfg.PlexRefreshAfterImport,
		PlexRefreshAfterPrune:          cfg.PlexRefreshAfterPrune,
		PlexRefreshDelay:               time.Duration(cfg.PlexRefreshDelaySeconds) * time.Second,
		PlexRefreshTimeout:             time.Duration(cfg.PlexRefreshTimeoutSeconds) * time.Second,
		PlexRefreshDedupe:              cfg.PlexRefreshDedupe,
		PlexMaxRefreshesPerRun:         cfg.PlexMaxRefreshesPerRun,
		SelfHealEnabled:                cfg.SelfHealEnabled,
		SelfHealInterval:               cfg.SelfHealInterval,
		SelfHealPlexRefreshAvailable:   cfg.SelfHealPlexRefreshAvailable,
		SelfHealPlexRecentHours:        cfg.SelfHealPlexRecentHours,
		SelfHealMaxPlexRefreshesPerRun: cfg.SelfHealMaxPlexRefreshesPerRun,
		Interval:                       cfg.ReconcileInterval,
		CSIWait:                        cfg.CSIWait,
		CSIVisibilityTimeout:           cfg.CSIVisibilityTimeout,
		CSIVisibilityPoll:              cfg.CSIVisibilityPoll,
		CSIVisibilityRetry:             cfg.CSIVisibilityRetry,
		ProviderCooldown:               cfg.ProviderCooldown,
		CacheGrace:                     cfg.CacheGrace,
		MaxRetries:                     cfg.MaxRetries,
		ConcurrentWorkers:              cfg.ConcurrentWorkers,
	})

	slog.Info("rehydrator starting",
		"config", cfg.ConfigPath,
		"tenant", cfg.Tenant,
		"interval", cfg.ReconcileInterval.String(),
		"cache_grace", cfg.CacheGrace.String(),
		"csi_path", cfg.CSIPath,
		"decypharr_url", cfg.DecypharrURL,
		"radarr_category", cfg.DecypharrRadarrCategory,
		"sonarr_category", cfg.DecypharrSonarrCategory,
		"delete_path", "torbox_by_infohash",
		"torbox_prune_enabled", cfg.TorBoxAPIKey != "",
		"prune_enabled", cfg.PruneEnabled,
		"rearm_enabled", cfg.RearmEnabled,
		"max_prunes_per_run", cfg.MaxPrunesPerRun,
		"max_rearms_per_run", cfg.MaxRearmsPerRun,
		"prune_wait_for_csi_gone", cfg.PruneWaitForCSIGone,
		"rearm_short_circuit_if_csi_visible", cfg.RearmShortCircuitIfCSIVisible,
		"csi_visibility_timeout", cfg.CSIVisibilityTimeout.String(),
		"csi_visibility_poll", cfg.CSIVisibilityPoll.String(),
		"csi_visibility_retry", cfg.CSIVisibilityRetry.String(),
		"provider_cooldown", cfg.ProviderCooldown.String(),
		"rclone_rc_enabled", cfg.RcloneRCEnabled,
		"rclone_rc_refresh_after_rearm", cfg.RcloneRCRefreshAfterRearm,
		"plex_enabled", cfg.PlexEnabled,
		"plex_url", cfg.PlexURL,
		"plex_movie_section_id", cfg.PlexMovieSectionID,
		"plex_tv_section_id", cfg.PlexTVSectionID,
		"plex_refresh_after_rearm", cfg.PlexRefreshAfterRearm,
		"plex_refresh_after_visibility", cfg.PlexRefreshAfterVisibility,
		"plex_refresh_after_import", cfg.PlexRefreshAfterImport,
		"plex_refresh_after_prune", cfg.PlexRefreshAfterPrune,
		"plex_refresh_delay", cfg.PlexRefreshDelaySeconds,
		"plex_max_refreshes_per_run", cfg.PlexMaxRefreshesPerRun,
		"plex_refresh_dedupe", cfg.PlexRefreshDedupe,
		"self_heal_enabled", cfg.SelfHealEnabled,
		"self_heal_interval", cfg.SelfHealInterval.String(),
		"self_heal_plex_refresh_available", cfg.SelfHealPlexRefreshAvailable,
		"self_heal_plex_recent_hours", cfg.SelfHealPlexRecentHours,
		"self_heal_max_plex_refreshes_per_run", cfg.SelfHealMaxPlexRefreshesPerRun,
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

	var radarrSyncer *syncer.RadarrSyncer
	var sonarrSyncer *syncer.SonarrSyncer
	var seerrSyncer *syncer.SeerrSyncer

	if cfg.RadarrSyncEnabled {
		radarrSyncer = syncer.NewRadarr(syncer.RadarrOptions{
			Repo:       repo,
			Radarr:     radarrClient,
			Tenant:     cfg.Tenant,
			Category:   cfg.DecypharrRadarrCategory,
			Interval:   cfg.RadarrSyncInterval,
			CacheGrace: cfg.CacheGrace,
			OnImported: func(syncCtx context.Context, item model.MediaCacheState, reason string) {
				ctrl.ScheduleInitialImportPlexRefresh(syncCtx, item, "radarr_"+reason)
			},
		})
	}

	if cfg.SonarrSyncEnabled {
		sonarrSyncer = syncer.NewSonarr(syncer.SonarrOptions{
			Repo:       repo,
			Sonarr:     sonarrClient,
			Tenant:     cfg.Tenant,
			Category:   cfg.DecypharrSonarrCategory,
			Interval:   cfg.SonarrSyncInterval,
			CacheGrace: cfg.CacheGrace,
			OnImported: func(syncCtx context.Context, item model.MediaCacheState, reason string) {
				ctrl.ScheduleInitialImportPlexRefresh(syncCtx, item, "sonarr_"+reason)
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

	var plexRefreshMovie func(context.Context, int) error
	var plexRefreshSeries func(context.Context, int) error
	var plexRefreshMovies func(context.Context) error
	var plexRefreshTV func(context.Context) error
	if plexClient != nil && plexClient.Configured() {
		plexRefreshMovie = func(ctx context.Context, arrID int) error {
			item, found, err := repo.GetState(ctx, cfg.Tenant, model.MediaMovie, arrID)
			if err != nil {
				return err
			}
			if !found {
				return os.ErrNotExist
			}
			targetPath := plexClient.TargetScanPath(item.SymlinkPath, model.MediaMovie)
			refreshCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.PlexRefreshTimeoutSeconds)*time.Second)
			defer cancel()
			slog.Info("manual Plex refresh starting", "arr_id", item.ArrID, "target_path", targetPath)
			err = plexClient.RefreshPathForMedia(refreshCtx, targetPath, model.MediaMovie)
			status := "success"
			message := ""
			if err != nil {
				status = "failed"
				message = err.Error()
				slog.Warn("manual Plex refresh failed", "arr_id", item.ArrID, "target_path", targetPath, "error", err)
			} else {
				slog.Info("manual Plex refresh complete", "arr_id", item.ArrID, "target_path", targetPath)
			}
			_ = repo.RecordPlexRefresh(ctx, cfg.Tenant, item.ID, item.ArrID, "manual_movie_refresh", "movie_folder", targetPath, status, message)
			return err
		}
		plexRefreshSeries = func(ctx context.Context, episodeFileID int) error {
			item, found, err := repo.GetState(ctx, cfg.Tenant, model.MediaSeries, episodeFileID)
			if err != nil {
				return err
			}
			if !found {
				return os.ErrNotExist
			}
			targetPath := plexClient.TargetScanPath(item.SymlinkPath, model.MediaSeries)
			refreshCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.PlexRefreshTimeoutSeconds)*time.Second)
			defer cancel()
			slog.Info("manual Plex series refresh starting", "episode_file_id", item.ArrID, "target_path", targetPath)
			err = plexClient.RefreshPathForMedia(refreshCtx, targetPath, model.MediaSeries)
			status := "success"
			message := ""
			if err != nil {
				status = "failed"
				message = err.Error()
				slog.Warn("manual Plex series refresh failed", "episode_file_id", item.ArrID, "target_path", targetPath, "error", err)
			} else {
				slog.Info("manual Plex series refresh complete", "episode_file_id", item.ArrID, "target_path", targetPath)
			}
			_ = repo.RecordPlexRefresh(ctx, cfg.Tenant, item.ID, item.ArrID, "manual_series_refresh", "series_folder", targetPath, status, message)
			return err
		}
		plexRefreshMovies = func(ctx context.Context) error {
			refreshCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.PlexRefreshTimeoutSeconds)*time.Second)
			defer cancel()
			slog.Warn("manual whole Plex movie section refresh requested")
			err := plexClient.RefreshMovies(refreshCtx)
			status := "success"
			message := ""
			if err != nil {
				status = "failed"
				message = err.Error()
			}
			_ = repo.RecordPlexRefresh(ctx, cfg.Tenant, "", 0, "manual_movies_refresh", "movie_section", "", status, message)
			return err
		}
		plexRefreshTV = func(ctx context.Context) error {
			refreshCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.PlexRefreshTimeoutSeconds)*time.Second)
			defer cancel()
			slog.Warn("manual whole Plex TV section refresh requested")
			err := plexClient.RefreshTV(refreshCtx)
			status := "success"
			message := ""
			if err != nil {
				status = "failed"
				message = err.Error()
			}
			_ = repo.RecordPlexRefresh(ctx, cfg.Tenant, "", 0, "manual_tv_refresh", "tv_section", "", status, message)
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
	if decypharrClient != nil && decypharrClient.Configured() {
		dependencyChecks["decypharr"] = decypharrClient.Ping
	}
	if seerrClient != nil && seerrClient.Configured() {
		dependencyChecks["seerr"] = func(ctx context.Context) error { _, err := seerrClient.Requests(ctx, 1); return err }
	}
	if plexClient != nil && plexClient.Configured() {
		dependencyChecks["plex"] = plexClient.Ping
	}

	var healthServer *health.Server
	if cfg.APIEnabled {
		healthServer = health.NewAPIServer(health.APIOptions{
			Addr:                         cfg.HealthAddr,
			Repo:                         repo,
			Tenant:                       cfg.Tenant,
			Token:                        cfg.APIToken,
			RequireToken:                 cfg.APIRequireToken,
			MetricsEnabled:               cfg.MetricsEnabled,
			PlaybackEnabled:              cfg.PlaybackEnabled,
			PlaybackRearmOnPlay:          cfg.PlaybackRearmOnPlay,
			PlaybackCooldown:             cfg.PlaybackCooldown,
			PlaybackIgnoredTitles:        cfg.PlaybackIgnoredTitles,
			PlaybackIgnoredTitleContains: cfg.PlaybackIgnoredTitleContains,
			RefreshRadarr:                refreshRadarr,
			RefreshSonarr:                refreshSonarr,
			RefreshSeerr:                 refreshSeerr,
			PlexRefreshMovie:             plexRefreshMovie,
			PlexRefreshSeries:            plexRefreshSeries,
			PlexRefreshMovies:            plexRefreshMovies,
			PlexRefreshTV:                plexRefreshTV,
			SelfHealRun: func(runCtx context.Context) error {
				ctrl.RunSelfHealOnce(runCtx)
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
