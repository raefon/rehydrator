package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
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

	mu          sync.Mutex
	running     bool
	lastStarted time.Time
	lastDone    time.Time
	lastSkipped time.Time
	debounce    time.Duration
}

type SonarrOptions struct {
	Repo       *db.Repo
	Sonarr     *arr.Client
	Tenant     string
	Category   string
	Interval   time.Duration
	CacheGrace time.Duration
	Debounce   time.Duration
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
	if opt.Debounce <= 0 {
		opt.Debounce = 30 * time.Second
	}
	return &SonarrSyncer{
		repo:       opt.Repo,
		sonarr:     opt.Sonarr,
		tenant:     opt.Tenant,
		category:   opt.Category,
		interval:   opt.Interval,
		cacheGrace: opt.CacheGrace,
		debounce:   opt.Debounce,
	}
}

func (s *SonarrSyncer) Run(ctx context.Context) {
	slog.Info("sonarr episode-file seed sync starting", "tenant", s.tenant, "interval", s.interval, "category", s.category, "debounce", s.debounce)
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

// SyncOnce performs a guarded Sonarr scan. It is safe to call from the scheduled
// worker and from webhook/API paths: overlapping scans are skipped, and repeated
// webhook bursts are naturally dampened by the debounce window.
func (s *SonarrSyncer) SyncOnce(ctx context.Context) error {
	return s.syncOnce(ctx, "manual_or_scheduled")
}

func (s *SonarrSyncer) SyncOnceSource(ctx context.Context, source string) error {
	return s.syncOnce(ctx, source)
}

func (s *SonarrSyncer) syncOnce(ctx context.Context, source string) error {
	now := time.Now()
	s.mu.Lock()
	if s.running {
		s.lastSkipped = now
		s.mu.Unlock()
		slog.Info("sonarr seed sync skipped; previous run still active", "source", source)
		return nil
	}
	if !s.lastStarted.IsZero() && now.Sub(s.lastStarted) < s.debounce {
		remaining := s.debounce - now.Sub(s.lastStarted)
		s.lastSkipped = now
		s.mu.Unlock()
		slog.Info("sonarr seed sync skipped by debounce", "source", source, "remaining", remaining)
		return nil
	}
	s.running = true
	s.lastStarted = now
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.lastDone = time.Now()
		s.mu.Unlock()
	}()

	return s.syncOnceUnlocked(ctx, source)
}

func (s *SonarrSyncer) syncOnceUnlocked(ctx context.Context, source string) error {
	series, err := s.sonarr.Series(ctx)
	if err != nil {
		slog.Error("sonarr seed sync failed to list series", "source", source, "error", err)
		return err
	}

	seriesSeen := len(series)
	episodesSeen := 0
	episodeFilesSeen := 0
	importedFiles := 0
	seeded := 0
	metadataResolved := 0

	for _, show := range series {
		episodes, err := s.sonarr.Episodes(ctx, show.ID)
		if err != nil {
			slog.Warn("sonarr seed sync failed to list episodes", "source", source, "series_id", show.ID, "series_title", show.Title, "error", err)
			episodes = nil
		} else {
			episodesSeen += len(episodes)
		}

		episodeByFileID := map[int]arr.Episode{}
		for _, episode := range episodes {
			if episode.EpisodeFileID > 0 {
				if _, exists := episodeByFileID[episode.EpisodeFileID]; !exists {
					episodeByFileID[episode.EpisodeFileID] = episode
				}
			}
			if episode.EpisodeFile != nil && episode.EpisodeFile.ID > 0 {
				if _, exists := episodeByFileID[episode.EpisodeFile.ID]; !exists {
					episodeByFileID[episode.EpisodeFile.ID] = episode
				}
			}
		}

		episodeFiles, err := s.sonarr.EpisodeFiles(ctx, show.ID)
		if err != nil {
			slog.Warn("sonarr seed sync failed to list episode files", "source", source, "series_id", show.ID, "series_title", show.Title, "error", err)
			continue
		}
		episodeFilesSeen += len(episodeFiles)

		for _, episodeFile := range episodeFiles {
			path := episodeFile.ImportedPath()
			if episodeFile.ID <= 0 || path == "" {
				continue
			}
			importedFiles++

			episode := episodeByFileID[episodeFile.ID]
			episodeID := episode.ID
			episodeTitle := episode.Title
			seasonNumber := firstInt(episode.SeasonNumber, episodeFile.SeasonNumber)
			episodeNumber := episode.EpisodeNumber

			item, err := s.repo.UpsertImportedEpisode(ctx, s.tenant, show.ID, episodeID, episodeFile.ID, show.Title, episodeTitle, seasonNumber, episodeNumber, path, s.category, time.Now().Add(s.cacheGrace), show.TVDBID)
			if err != nil {
				slog.Warn("sonarr seed sync failed to upsert episode file", "source", source, "series_id", show.ID, "episode_file_id", episodeFile.ID, "series_title", show.Title, "episode_title", episodeTitle, "path", path, "error", err)
				continue
			}
			seeded++

			if item.InfoHash == nil || *item.InfoHash == "" {
				torrent, err := s.sonarr.LatestGrabbedTorrentForState(ctx, item)
				if err != nil {
					slog.Debug("sonarr seed sync could not resolve torrent metadata", "source", source, "series_id", show.ID, "episode_file_id", episodeFile.ID, "series_title", show.Title, "episode_title", episodeTitle, "error", err)
					continue
				}
				if err := s.repo.SaveTorrentMetadata(ctx, item.ID, torrent, s.category); err != nil {
					slog.Warn("sonarr seed sync failed to save torrent metadata", "source", source, "series_id", show.ID, "episode_file_id", episodeFile.ID, "series_title", show.Title, "error", err)
					continue
				}
				payload, _ := json.Marshal(map[string]string{
					"arr":             "sonarr",
					"series_title":    show.Title,
					"episode_title":   episodeTitle,
					"series_id":       fmt.Sprintf("%d", show.ID),
					"episode_id":      fmt.Sprintf("%d", episodeID),
					"episode_file_id": fmt.Sprintf("%d", episodeFile.ID),
					"season_number":   fmt.Sprintf("%d", seasonNumber),
					"episode_number":  fmt.Sprintf("%d", episodeNumber),
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

	slog.Info("sonarr seed sync complete", "source", source, "series", seriesSeen, "episodes", episodesSeen, "episode_files", episodeFilesSeen, "imported_episode_files", importedFiles, "seeded", seeded, "metadata_resolved", metadataResolved)
	return nil
}

func firstInt(vs ...int) int {
	for _, v := range vs {
		if v != 0 {
			return v
		}
	}
	return 0
}

var _ = model.MediaSeries
