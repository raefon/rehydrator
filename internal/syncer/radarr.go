package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
)

type RadarrSyncer struct {
	repo       *db.Repo
	radarr     *arr.Client
	tenant     string
	category   string
	interval   time.Duration
	cacheGrace time.Duration
}

type RadarrOptions struct {
	Repo       *db.Repo
	Radarr     *arr.Client
	Tenant     string
	Category   string
	Interval   time.Duration
	CacheGrace time.Duration
}

func NewRadarr(opt RadarrOptions) *RadarrSyncer {
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Minute
	}
	if opt.CacheGrace <= 0 {
		opt.CacheGrace = 24 * time.Hour
	}
	if opt.Category == "" {
		opt.Category = "radarr"
	}
	return &RadarrSyncer{
		repo:       opt.Repo,
		radarr:     opt.Radarr,
		tenant:     opt.Tenant,
		category:   opt.Category,
		interval:   opt.Interval,
		cacheGrace: opt.CacheGrace,
	}
}

func (s *RadarrSyncer) Run(ctx context.Context) {
	slog.Info("radarr seed sync starting", "tenant", s.tenant, "interval", s.interval, "category", s.category)
	_ = s.SyncOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("radarr seed sync stopped")
			return
		case <-ticker.C:
			_ = s.SyncOnce(ctx)
		}
	}
}

func (s *RadarrSyncer) SyncOnce(ctx context.Context) error {
	movies, err := s.radarr.Movies(ctx)
	if err != nil {
		slog.Error("radarr seed sync failed to list movies", "error", err)
		return err
	}

	imported := 0
	seeded := 0
	metadataResolved := 0

	for _, movie := range movies {
		path := movie.ImportedPath()
		if path == "" {
			continue
		}
		imported++

		item, err := s.repo.UpsertImportedMovie(ctx, s.tenant, movie.ID, movie.Title, path, s.category, time.Now().Add(s.cacheGrace), movie.TMDBID, 0)
		if err != nil {
			slog.Warn("radarr seed sync failed to upsert movie", "movie_id", movie.ID, "title", movie.Title, "error", err)
			continue
		}
		seeded++

		if item.InfoHash == nil || *item.InfoHash == "" {
			torrent, err := s.radarr.LatestGrabbedTorrent(ctx, movie.ID, model.MediaMovie)
			if err != nil {
				slog.Warn("radarr seed sync could not resolve torrent metadata", "movie_id", movie.ID, "title", movie.Title, "error", err)
				continue
			}
			if err := s.repo.SaveTorrentMetadata(ctx, item.ID, torrent, s.category); err != nil {
				slog.Warn("radarr seed sync failed to save torrent metadata", "movie_id", movie.ID, "title", movie.Title, "error", err)
				continue
			}
			payload, _ := json.Marshal(map[string]string{
				"arr":          "radarr",
				"arr_title":    movie.Title,
				"tmdb_id":      fmt.Sprintf("%d", movie.TMDBID),
				"infohash":     torrent.InfoHash,
				"source_title": torrent.SourceTitle,
				"category":     s.category,
			})
			_ = s.repo.Event(ctx, item.ID, "radarr_seed_metadata_resolved", string(payload))
			metadataResolved++
		}
	}

	slog.Info("radarr seed sync complete", "movies", len(movies), "imported", imported, "seeded", seeded, "metadata_resolved", metadataResolved)
	return nil
}
