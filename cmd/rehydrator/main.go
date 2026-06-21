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
	"github.com/raefon/rehydrator/internal/health"
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

	ctrl := controller.New(controller.Options{
		Repo:              repo,
		Radarr:            arr.NewClient("radarr", cfg.RadarrURL, cfg.RadarrAPIKey),
		Sonarr:            arr.NewClient("sonarr", cfg.SonarrURL, cfg.SonarrAPIKey),
		TorBox:            torbox.NewClient(cfg.TorBoxAPIKey),
		CSI:               csi.NewChecker(cfg.CSIPath),
		Interval:          cfg.ReconcileInterval,
		CSIWait:           cfg.CSIWait,
		CacheGrace:        cfg.CacheGrace,
		MaxRetries:        cfg.MaxRetries,
		ConcurrentWorkers: cfg.ConcurrentWorkers,
	})

	slog.Info("rehydrator starting",
		"config", cfg.ConfigPath,
		"interval", cfg.ReconcileInterval.String(),
		"cache_grace", cfg.CacheGrace.String(),
		"csi_path", cfg.CSIPath,
		"workers", cfg.ConcurrentWorkers,
		"health_addr", cfg.HealthAddr,
	)

	healthServer := health.New(cfg.HealthAddr)
	go healthServer.Run(ctx)

	if err := ctrl.Run(ctx); err != nil {
		slog.Error("controller stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("rehydrator stopped")
}
