package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/controller"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/health"
	"github.com/raefon/rehydrator/internal/rclone"
	"github.com/raefon/rehydrator/internal/seerr"
	"github.com/raefon/rehydrator/internal/syncer"
	"github.com/raefon/rehydrator/internal/torbox"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

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

	ctrl := controller.New(controller.Options{
		Tenant:                     cfg.Tenant,
		Repo:                       repo,
		Radarr:                     radarrClient,
		Sonarr:                     sonarrClient,
		Decypharr:                  decypharrClient,
		TorBox:                     torboxClient,
		CSI:                        csi.NewChecker(cfg.CSIPath),
		Rclone:                     rcloneClient,
		RadarrCategory:             cfg.DecypharrRadarrCategory,
		SonarrCategory:             cfg.DecypharrSonarrCategory,
		DeleteFilesOnPrune:         cfg.DecypharrDeleteFilesOnPrune,
		PruneEnabled:               cfg.PruneEnabled,
		RearmEnabled:               cfg.RearmEnabled,
		MaxPrunesPerRun:            cfg.MaxPrunesPerRun,
		MaxRearmsPerRun:            cfg.MaxRearmsPerRun,
		PruneWaitForCSIGone:        cfg.PruneWaitForCSIGone,
		RearmShortCircuitIfVisible: cfg.RearmShortCircuitIfCSIVisible,
		RcloneRefreshAfterRearm:    cfg.RcloneRCRefreshAfterRearm,
		Interval:                   cfg.ReconcileInterval,
		CSIWait:                    cfg.CSIWait,
		CSIVisibilityTimeout:       cfg.CSIVisibilityTimeout,
		CSIVisibilityPoll:          cfg.CSIVisibilityPoll,
		CSIVisibilityRetry:         cfg.CSIVisibilityRetry,
		ProviderCooldown:           cfg.ProviderCooldown,
		CacheGrace:                 cfg.CacheGrace,
		MaxRetries:                 cfg.MaxRetries,
		ConcurrentWorkers:          cfg.ConcurrentWorkers,
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
		"seerr_url", cfg.SeerrURL,
		"seerr_sync_enabled", cfg.SeerrSyncEnabled,
		"seerr_sync_interval", cfg.SeerrSyncInterval.String(),
		"seerr_sync_limit", cfg.SeerrSyncLimit,
		"workers", cfg.ConcurrentWorkers,
	)

	var radarrSyncer *syncer.RadarrSyncer
	var seerrSyncer *syncer.SeerrSyncer

	if cfg.RadarrSyncEnabled {
		radarrSyncer = syncer.NewRadarr(syncer.RadarrOptions{
			Repo:       repo,
			Radarr:     radarrClient,
			Tenant:     cfg.Tenant,
			Category:   cfg.DecypharrRadarrCategory,
			Interval:   cfg.RadarrSyncInterval,
			CacheGrace: cfg.CacheGrace,
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
	var refreshSeerr func(context.Context) error
	if seerrSyncer != nil {
		refreshSeerr = seerrSyncer.SyncOnce
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
			RefreshSeerr:                 refreshSeerr,
		})
	} else {
		healthServer = health.NewServer(cfg.HealthAddr)
	}
	go healthServer.Run(ctx)

	if radarrSyncer != nil {
		go radarrSyncer.Run(ctx)
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
