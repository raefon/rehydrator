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

type SonarrSyncer struct {
	repo       *db.Repo
	sonarr     *arr.Client
	tenant     string
	category   string
	interval   time.Duration
	cacheGrace time.Duration
}

type SonarrOptions struct {
	Repo       *db.Repo
	Sonarr     *arr.Client
	Tenant     string
	Category   string
	Interval   time.Duration
	CacheGrace time.Duration
}

func NewSonarr(opt SonarrOptions) *SonarrSyncer {
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Minute
	}
	if opt.CacheGrace <= 0 {
		opt.CacheGrace = 24 * time.Hour
	}
	if opt.Category == "" {
		opt.Category = "sonarr"
	}
	return &SonarrSyncer{
		repo:       opt.Repo,
		sonarr:     opt.Sonarr,
		tenant:     opt.Tenant,
		category:   opt.Category,
		interval:   opt.Interval,
		cacheGrace: opt.CacheGrace,
	}
}

func (s *SonarrSyncer) Run(ctx context.Context) {
	slog.Info("sonarr episode-file seed sync starting", "tenant", s.tenant, "interval", s.interval, "category", s.category)
	_ = s.SyncOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("sonarr episode-file seed sync stopped")
			return
		case <-ticker.C:
			_ = s.SyncOnce(ctx)
		}
	}
}

func (s *SonarrSyncer) SyncOnce(ctx context.Context) error {
	series, err := s.sonarr.Series(ctx)
	if err != nil {
		slog.Error("sonarr seed sync failed to list series", "error", err)
		return err
	}

	seriesSeen := len(series)
	episodesSeen := 0
	importedFiles := 0
	seeded := 0
	metadataResolved := 0

	for _, show := range series {
		episodes, err := s.sonarr.Episodes(ctx, show.ID)
		if err != nil {
			slog.Warn("sonarr seed sync failed to list episodes", "series_id", show.ID, "series_title", show.Title, "error", err)
			continue
		}
		episodesSeen += len(episodes)

		seenEpisodeFiles := map[int]struct{}{}
		for _, episode := range episodes {
			path := episode.ImportedPath()
			if episode.EpisodeFileID <= 0 || path == "" {
				continue
			}
			// One row per episode file. Multi-episode files share episodeFileId;
			// keep the first episode's labels for the row and avoid duplicate upserts.
			if _, ok := seenEpisodeFiles[episode.EpisodeFileID]; ok {
				continue
			}
			seenEpisodeFiles[episode.EpisodeFileID] = struct{}{}
			importedFiles++

			item, err := s.repo.UpsertImportedEpisode(ctx, s.tenant, show.ID, episode.ID, episode.EpisodeFileID, show.Title, episode.Title, episode.SeasonNumber, episode.EpisodeNumber, path, s.category, time.Now().Add(s.cacheGrace), show.TVDBID)
			if err != nil {
				slog.Warn("sonarr seed sync failed to upsert episode file", "series_id", show.ID, "episode_file_id", episode.EpisodeFileID, "series_title", show.Title, "episode_title", episode.Title, "error", err)
				continue
			}
			seeded++

			if item.InfoHash == nil || *item.InfoHash == "" {
				torrent, err := s.sonarr.LatestGrabbedTorrentForState(ctx, item)
				if err != nil {
					slog.Warn("sonarr seed sync could not resolve torrent metadata", "series_id", show.ID, "episode_file_id", episode.EpisodeFileID, "series_title", show.Title, "episode_title", episode.Title, "error", err)
					continue
				}
				if err := s.repo.SaveTorrentMetadata(ctx, item.ID, torrent, s.category); err != nil {
					slog.Warn("sonarr seed sync failed to save torrent metadata", "series_id", show.ID, "episode_file_id", episode.EpisodeFileID, "series_title", show.Title, "error", err)
					continue
				}
				payload, _ := json.Marshal(map[string]string{
					"arr":             "sonarr",
					"series_title":    show.Title,
					"episode_title":   episode.Title,
					"series_id":       fmt.Sprintf("%d", show.ID),
					"episode_id":      fmt.Sprintf("%d", episode.ID),
					"episode_file_id": fmt.Sprintf("%d", episode.EpisodeFileID),
					"season_number":   fmt.Sprintf("%d", episode.SeasonNumber),
					"episode_number":  fmt.Sprintf("%d", episode.EpisodeNumber),
					"tvdb_id":         fmt.Sprintf("%d", show.TVDBID),
					"infohash":        torrent.InfoHash,
					"source_title":    torrent.SourceTitle,
					"category":        s.category,
				})
				_ = s.repo.Event(ctx, item.ID, "sonarr_seed_metadata_resolved", string(payload))
				metadataResolved++
			}
		}
	}

	slog.Info("sonarr seed sync complete", "series", seriesSeen, "episodes", episodesSeen, "imported_episode_files", importedFiles, "seeded", seeded, "metadata_resolved", metadataResolved)
	return nil
}

var _ = model.MediaSeries
