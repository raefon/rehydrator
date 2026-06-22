package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/model"
)

type Options struct {
	Repo      *db.Repo
	Radarr    *arr.Client
	Sonarr    *arr.Client
	Decypharr *decypharr.Client
	CSI       *csi.Checker

	RadarrCategory     string
	SonarrCategory     string
	DeleteFilesOnPrune bool
	Interval           time.Duration
	CSIWait            time.Duration
	CacheGrace         time.Duration
	MaxRetries         int
	ConcurrentWorkers  int
}

type Controller struct {
	opt Options
}

func New(opt Options) *Controller {
	if opt.ConcurrentWorkers <= 0 {
		opt.ConcurrentWorkers = 4
	}
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Second
	}
	if opt.CSIWait <= 0 {
		opt.CSIWait = 300 * time.Second
	}
	if opt.CacheGrace <= 0 {
		opt.CacheGrace = 24 * time.Hour
	}
	if opt.MaxRetries <= 0 {
		opt.MaxRetries = 10
	}
	if opt.RadarrCategory == "" {
		opt.RadarrCategory = "radarr"
	}
	if opt.SonarrCategory == "" {
		opt.SonarrCategory = "sonarr"
	}
	return &Controller{opt: opt}
}

func (c *Controller) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.opt.Interval)
	defer ticker.Stop()

	c.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) {
	c.reconcileRearms(ctx)
	c.reconcilePrunes(ctx)
}

func (c *Controller) reconcileRearms(ctx context.Context) {
	items, err := c.opt.Repo.RearmWorkItems(ctx, c.opt.ConcurrentWorkers, c.opt.MaxRetries)
	if err != nil {
		slog.Error("failed to load rearm work items", "error", err)
		return
	}
	c.runItems(ctx, items, c.handleRearm)
}

func (c *Controller) reconcilePrunes(ctx context.Context) {
	items, err := c.opt.Repo.PruneWorkItems(ctx, c.opt.ConcurrentWorkers)
	if err != nil {
		slog.Error("failed to load prune work items", "error", err)
		return
	}
	c.runItems(ctx, items, c.handlePrune)
}

func (c *Controller) runItems(ctx context.Context, items []model.MediaCacheState, fn func(context.Context, model.MediaCacheState)) {
	if len(items) == 0 {
		return
	}

	sem := make(chan struct{}, c.opt.ConcurrentWorkers)
	var wg sync.WaitGroup

	for _, item := range items {
		item := item
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, item)
		}()
	}

	wg.Wait()
}

func (c *Controller) handleRearm(ctx context.Context, item model.MediaCacheState) {
	log := slog.With(
		"tenant", item.Tenant,
		"media_type", item.MediaType,
		"arr_id", item.ArrID,
		"path", item.SymlinkPath,
		"state", item.State,
	)

	if c.opt.CSI.Exists(item.SymlinkPath) {
		log.Info("file already visible through CSI; marking available")
		cachedUntil := time.Now().Add(c.opt.CacheGrace)
		_ = c.opt.Repo.MarkAvailable(ctx, item.ID, valueOrEmpty(item.InfoHash), cachedUntil)
		return
	}

	log.Info("file missing; rearming through Decypharr")
	if err := c.opt.Repo.MarkRearming(ctx, item.ID); err != nil {
		log.Error("failed to mark rearming", "error", err)
		return
	}

	arrClient, err := c.arrClientFor(item.MediaType)
	if err != nil {
		c.fail(ctx, item, err)
		return
	}

	torrent, err := arrClient.LatestGrabbedTorrent(ctx, item.ArrID, item.MediaType)
	if err != nil {
		c.fail(ctx, item, err)
		return
	}

	category := c.categoryFor(item.MediaType)
	if err := c.opt.Repo.SaveTorrentMetadata(ctx, item.ID, torrent, category); err != nil {
		c.fail(ctx, item, err)
		return
	}

	metaJSON, _ := json.Marshal(map[string]string{
		"infohash":     torrent.InfoHash,
		"source":       torrent.Source,
		"source_title": torrent.SourceTitle,
		"download_id":  torrent.DownloadID,
		"category":     category,
		"client":       "decypharr",
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "torrent_metadata_resolved", string(metaJSON))

	addResult, err := c.opt.Decypharr.AddTorrent(ctx, torrent, category)
	if err != nil {
		c.fail(ctx, item, err)
		return
	}

	addJSON, _ := json.Marshal(map[string]string{
		"infohash": addResult.Hash,
		"category": category,
		"client":   "decypharr",
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "decypharr_readd_requested", string(addJSON))

	if ok := c.waitForCSI(ctx, item.SymlinkPath); !ok {
		c.fail(ctx, item, fmt.Errorf("CSI path did not appear within %s", c.opt.CSIWait))
		return
	}

	cachedUntil := time.Now().Add(c.opt.CacheGrace)
	if err := c.opt.Repo.MarkAvailable(ctx, item.ID, firstNonEmpty(addResult.Hash, torrent.InfoHash), cachedUntil); err != nil {
		log.Error("failed to mark available", "error", err)
		return
	}

	finalJSON, _ := json.Marshal(map[string]string{
		"cached_until": cachedUntil.Format(time.RFC3339),
		"infohash":     firstNonEmpty(addResult.Hash, torrent.InfoHash),
		"client":       "decypharr",
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "available", string(finalJSON))
	log.Info("rehydration complete", "cached_until", cachedUntil, "infohash", firstNonEmpty(addResult.Hash, torrent.InfoHash))
}

func (c *Controller) handlePrune(ctx context.Context, item model.MediaCacheState) {
	log := slog.With(
		"tenant", item.Tenant,
		"media_type", item.MediaType,
		"arr_id", item.ArrID,
		"infohash", valueOrEmpty(item.InfoHash),
		"download_client", valueOrEmpty(item.DownloadClient),
	)

	infoHash := valueOrEmpty(item.InfoHash)
	if infoHash == "" {
		log.Warn("no infohash available; marking archived without download-client delete")
		_ = c.opt.Repo.MarkArchived(ctx, item.ID)
		return
	}

	log.Info("pruning expired Decypharr torrent")
	if err := c.opt.Repo.MarkPruning(ctx, item.ID); err != nil {
		log.Error("failed to mark pruning", "error", err)
		return
	}

	if err := c.opt.Decypharr.DeleteTorrent(ctx, infoHash, c.opt.DeleteFilesOnPrune); err != nil {
		c.fail(ctx, item, err)
		return
	}

	deleteJSON, _ := json.Marshal(map[string]string{
		"infohash":     infoHash,
		"client":       "decypharr",
		"delete_files": fmt.Sprintf("%t", c.opt.DeleteFilesOnPrune),
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "decypharr_deleted", string(deleteJSON))

	if err := c.opt.Repo.MarkArchived(ctx, item.ID); err != nil {
		log.Error("failed to mark archived", "error", err)
		return
	}

	_ = c.opt.Repo.Event(ctx, item.ID, "archived", "{}")
	log.Info("prune complete; item archived")
}

func (c *Controller) arrClientFor(mediaType model.MediaType) (*arr.Client, error) {
	switch mediaType {
	case model.MediaMovie:
		return c.opt.Radarr, nil
	case model.MediaSeries:
		return c.opt.Sonarr, nil
	default:
		return nil, fmt.Errorf("unsupported media_type: %s", mediaType)
	}
}

func (c *Controller) categoryFor(mediaType model.MediaType) string {
	switch mediaType {
	case model.MediaMovie:
		return c.opt.RadarrCategory
	case model.MediaSeries:
		return c.opt.SonarrCategory
	default:
		return ""
	}
}

func (c *Controller) waitForCSI(ctx context.Context, path string) bool {
	deadline := time.Now().Add(c.opt.CSIWait)
	for time.Now().Before(deadline) {
		if c.opt.CSI.Exists(path) {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Second):
		}
	}
	return false
}

func (c *Controller) fail(ctx context.Context, item model.MediaCacheState, err error) {
	slog.Error("operation failed",
		"tenant", item.Tenant,
		"media_type", item.MediaType,
		"arr_id", item.ArrID,
		"error", err,
	)
	_ = c.opt.Repo.MarkFailed(ctx, item.ID, err.Error(), c.opt.MaxRetries)
	payload, _ := json.Marshal(map[string]string{"error": err.Error()})
	_ = c.opt.Repo.Event(ctx, item.ID, "failed", string(payload))
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
