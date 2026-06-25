package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/rclone"
	"github.com/raefon/rehydrator/internal/torbox"
)

type Options struct {
	Tenant    string
	Repo      *db.Repo
	Radarr    *arr.Client
	Sonarr    *arr.Client
	Decypharr *decypharr.Client
	TorBox    *torbox.Client
	CSI       *csi.Checker
	Rclone    *rclone.Client

	RadarrCategory             string
	SonarrCategory             string
	DeleteFilesOnPrune         bool
	PruneEnabled               bool
	RearmEnabled               bool
	MaxPrunesPerRun            int
	MaxRearmsPerRun            int
	PruneWaitForCSIGone        bool
	RearmShortCircuitIfVisible bool
	RcloneRefreshAfterRearm    bool
	Interval                   time.Duration
	CSIWait                    time.Duration
	CSIVisibilityTimeout       time.Duration
	CSIVisibilityPoll          time.Duration
	CSIVisibilityRetry         time.Duration
	ProviderCooldown           time.Duration
	CacheGrace                 time.Duration
	MaxRetries                 int
	ConcurrentWorkers          int
}

type Controller struct {
	opt Options
}

func New(opt Options) *Controller {
	if opt.ConcurrentWorkers <= 0 {
		opt.ConcurrentWorkers = 4
	}
	if opt.MaxPrunesPerRun <= 0 {
		opt.MaxPrunesPerRun = opt.ConcurrentWorkers
	}
	if opt.MaxRearmsPerRun <= 0 {
		opt.MaxRearmsPerRun = opt.ConcurrentWorkers
	}
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Second
	}
	if opt.CSIWait <= 0 {
		opt.CSIWait = 15 * time.Minute
	}
	if opt.CSIVisibilityTimeout <= 0 {
		opt.CSIVisibilityTimeout = opt.CSIWait
	}
	if opt.CSIVisibilityPoll <= 0 {
		opt.CSIVisibilityPoll = 10 * time.Second
	}
	if opt.CSIVisibilityRetry <= 0 {
		opt.CSIVisibilityRetry = 60 * time.Second
	}
	if opt.ProviderCooldown <= 0 {
		opt.ProviderCooldown = 15 * time.Minute
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
	if c.opt.RearmEnabled {
		c.reconcileVisibility(ctx)
		c.reconcileRearms(ctx)
	}
	if c.opt.PruneEnabled {
		c.reconcilePrunes(ctx)
	}
}

func (c *Controller) reconcileRearms(ctx context.Context) {
	if c.providerCoolingDown(ctx, "decypharr") {
		return
	}
	items, err := c.opt.Repo.RearmWorkItems(ctx, c.opt.MaxRearmsPerRun, c.opt.MaxRetries)
	if err != nil {
		slog.Error("failed to load rearm work items", "error", err)
		return
	}
	c.runItems(ctx, items, c.handleRearm)
}

func (c *Controller) reconcileVisibility(ctx context.Context) {
	items, err := c.opt.Repo.VisibilityWorkItems(ctx, c.opt.MaxRearmsPerRun)
	if err != nil {
		slog.Error("failed to load CSI visibility work items", "error", err)
		return
	}
	c.runItems(ctx, items, c.handleVisibility)
}

func (c *Controller) reconcilePrunes(ctx context.Context) {
	if c.providerCoolingDown(ctx, "torbox") {
		return
	}
	items, err := c.opt.Repo.PruneWorkItems(ctx, c.opt.MaxPrunesPerRun)
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

	pathVisible := c.opt.CSI.Exists(item.SymlinkPath)
	if pathVisible && c.opt.RearmShortCircuitIfVisible {
		log.Info("file already visible through CSI; marking available", "rearm_short_circuit_if_visible", true)
		cachedUntil := time.Now().Add(c.opt.CacheGrace)
		_ = c.opt.Repo.MarkAvailable(ctx, item.ID, valueOrEmpty(item.InfoHash), cachedUntil)
		return
	}

	if pathVisible {
		log.Info("CSI path is visible but rearm will still be queued because CSI visibility is not authoritative for archived cache state")
	} else {
		log.Info("file missing; rearming through Decypharr")
	}
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
		c.failProvider(ctx, item, "decypharr", err)
		return
	}

	addJSON, _ := json.Marshal(map[string]string{
		"infohash": addResult.Hash,
		"category": category,
		"client":   "decypharr",
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "decypharr_readd_requested", string(addJSON))
	c.refreshRclone(ctx, item.SymlinkPath)

	if ok := c.waitForCSI(ctx, item.SymlinkPath); !ok {
		c.markWaitingVisibility(ctx, item, firstNonEmpty(addResult.Hash, torrent.InfoHash), fmt.Sprintf("CSI path did not appear within %s after Decypharr add; waiting for mount visibility", c.opt.CSIVisibilityTimeout))
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
		c.fail(ctx, item, fmt.Errorf("cannot prune: missing infohash"))
		return
	}

	if c.opt.TorBox == nil || !c.opt.TorBox.Configured() {
		c.fail(ctx, item, fmt.Errorf("cannot prune: missing TorBox API key/client"))
		return
	}

	log.Info("pruning expired TorBox torrent by infohash")
	if err := c.opt.Repo.MarkPruning(ctx, item.ID); err != nil {
		log.Error("failed to mark pruning", "error", err)
		return
	}

	torrent, found, err := c.opt.TorBox.FindTorrentByHash(ctx, infoHash)
	if err != nil {
		c.failProvider(ctx, item, "torbox", err)
		return
	}

	if !found {
		if c.opt.CSI.Exists(item.SymlinkPath) {
			log.Warn("TorBox torrent not found but CSI path is still visible; marking archived because provider absence is authoritative")
			_ = c.opt.Repo.Event(ctx, item.ID, "torbox_missing_csi_visible_ignored", `{}`)
		} else {
			log.Warn("TorBox torrent not found and CSI path is already gone; marking archived")
			_ = c.opt.Repo.Event(ctx, item.ID, "torbox_missing_path_absent", `{}`)
		}

		if err := c.opt.Repo.MarkArchived(ctx, item.ID); err != nil {
			log.Error("failed to mark archived", "error", err)
		}
		return
	}

	if torrent.ID == "" {
		c.fail(ctx, item, fmt.Errorf("TorBox torrent matched infohash %s but had no torrent ID", infoHash))
		return
	}

	if err := c.opt.Repo.SaveTorBoxTorrentID(ctx, item.ID, torrent.ID); err != nil {
		log.Warn("failed to save TorBox torrent ID", "error", err, "torbox_torrent_id", torrent.ID)
	}

	if err := c.opt.TorBox.DeleteTorrent(ctx, torrent.ID); err != nil {
		c.failProvider(ctx, item, "torbox", err)
		return
	}

	deleteJSON, _ := json.Marshal(map[string]string{
		"infohash":          infoHash,
		"client":            "torbox",
		"torbox_torrent_id": torrent.ID,
		"name":              torrent.Name,
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "torbox_deleted", string(deleteJSON))

	if c.opt.PruneWaitForCSIGone {
		if ok := c.waitForCSIGone(ctx, item.SymlinkPath); !ok {
			log.Warn("TorBox torrent deleted but CSI path still existed after wait; marking archived because provider delete is authoritative", "wait", c.opt.CSIWait)
			_ = c.opt.Repo.Event(ctx, item.ID, "csi_path_still_visible_after_prune", `{}`)
		}
	} else if c.opt.CSI.Exists(item.SymlinkPath) {
		log.Info("CSI path still visible after TorBox delete; marking archived because prune_wait_for_csi_gone=false")
		_ = c.opt.Repo.Event(ctx, item.ID, "csi_path_visible_ignored_after_prune", `{}`)
	}

	if err := c.opt.Repo.MarkArchived(ctx, item.ID); err != nil {
		log.Error("failed to mark archived", "error", err)
		return
	}

	_ = c.opt.Repo.Event(ctx, item.ID, "archived", "{}")
	log.Info("prune complete; TorBox torrent deleted and item archived", "torbox_torrent_id", torrent.ID)
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

func (c *Controller) handleVisibility(ctx context.Context, item model.MediaCacheState) {
	log := slog.With(
		"tenant", item.Tenant,
		"media_type", item.MediaType,
		"arr_id", item.ArrID,
		"path", item.SymlinkPath,
	)
	c.refreshRclone(ctx, item.SymlinkPath)
	if !c.opt.CSI.Exists(item.SymlinkPath) {
		msg := "waiting for CSI/rclone mount visibility after successful Decypharr add"
		if valueOrEmpty(item.LastError) != "" {
			msg = valueOrEmpty(item.LastError)
		}
		if err := c.opt.Repo.MarkWaitingVisibility(ctx, item.ID, msg, time.Now().Add(c.opt.CSIVisibilityRetry)); err != nil {
			log.Error("failed to keep item waiting for CSI visibility", "error", err)
			return
		}
		log.Info("CSI path still not visible; will retry visibility check", "retry_in", c.opt.CSIVisibilityRetry)
		return
	}

	cachedUntil := time.Now().Add(c.opt.CacheGrace)
	if err := c.opt.Repo.MarkAvailable(ctx, item.ID, valueOrEmpty(item.InfoHash), cachedUntil); err != nil {
		log.Error("failed to mark available after CSI visibility", "error", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"cached_until": cachedUntil.Format(time.RFC3339), "infohash": valueOrEmpty(item.InfoHash)})
	_ = c.opt.Repo.Event(ctx, item.ID, "available_after_visibility_wait", string(payload))
	log.Info("CSI path appeared; item marked available", "cached_until", cachedUntil)
}

func (c *Controller) markWaitingVisibility(ctx context.Context, item model.MediaCacheState, infoHash string, msg string) {
	log := slog.With("tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "path", item.SymlinkPath)
	next := time.Now().Add(c.opt.CSIVisibilityRetry)
	if err := c.opt.Repo.MarkWaitingVisibility(ctx, item.ID, msg, next); err != nil {
		log.Error("failed to mark waiting for CSI visibility", "error", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"error": msg, "infohash": infoHash, "next_retry_at": next.Format(time.RFC3339)})
	_ = c.opt.Repo.Event(ctx, item.ID, "waiting_for_visibility", string(payload))
	log.Warn("Decypharr add succeeded but CSI path is not visible yet; waiting instead of failing", "next_retry_at", next, "infohash", infoHash)
}

func (c *Controller) refreshRclone(ctx context.Context, path string) {
	if !c.opt.RcloneRefreshAfterRearm || c.opt.Rclone == nil || !c.opt.Rclone.Configured() {
		return
	}
	if err := c.opt.Rclone.RefreshForPath(ctx, path); err != nil {
		slog.Warn("rclone rc refresh failed", "path", path, "error", err)
		return
	}
	slog.Info("rclone rc refresh requested", "path", path)
}

func (c *Controller) providerCoolingDown(ctx context.Context, provider string) bool {
	if c.opt.Repo == nil {
		return false
	}
	tenant := c.opt.Tenant
	active, until, reason, err := c.opt.Repo.ProviderCooldownActive(ctx, tenant, provider)
	if err != nil {
		slog.Warn("failed to check provider cooldown", "provider", provider, "error", err)
		return false
	}
	if active {
		slog.Warn("provider cooldown active; skipping work", "provider", provider, "cooldown_until", until, "reason", reason)
	}
	return active
}

func (c *Controller) failProvider(ctx context.Context, item model.MediaCacheState, provider string, err error) {
	if isRateLimited(err) && c.opt.Repo != nil {
		tenant := item.Tenant
		if tenant == "" {
			tenant = c.opt.Tenant
		}
		_ = c.opt.Repo.SetProviderCooldown(ctx, tenant, provider, c.opt.ProviderCooldown, err.Error())
		slog.Warn("provider rate limit detected; cooldown set", "provider", provider, "cooldown", c.opt.ProviderCooldown, "error", err)
	}
	c.fail(ctx, item, err)
}

func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "ratelimit")
}

func (c *Controller) waitForCSI(ctx context.Context, path string) bool {
	deadline := time.Now().Add(c.opt.CSIVisibilityTimeout)
	for time.Now().Before(deadline) {
		if c.opt.CSI.Exists(path) {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(c.opt.CSIVisibilityPoll):
		}
	}
	return c.opt.CSI.Exists(path)
}

func (c *Controller) waitForCSIGone(ctx context.Context, path string) bool {
	deadline := time.Now().Add(c.opt.CSIWait)
	for time.Now().Before(deadline) {
		if !c.opt.CSI.Exists(path) {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Second):
		}
	}
	return !c.opt.CSI.Exists(path)
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
