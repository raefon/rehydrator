package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/rclone"
)

type Options struct {
	FileHealth     config.FileHealthConfig
	Jellyfin       *jellyfin.Client
	JellyfinConfig config.JellyfinConfig
	Tenant         string
	Repo           *db.Repo
	Radarr         *arr.Client
	Sonarr         *arr.Client
	Decypharr      *decypharr.Client
	CSI            *csi.Checker
	Rclone         *rclone.Client

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
	SelfHealEnabled            bool
	SelfHealInterval           time.Duration
	SelfHealRefreshAvailable   bool
	SelfHealRecentHours        int
	SelfHealMaxRefreshesPerRun int
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
	healthProbe             *csi.ReadProbe
	fileProbe               *csi.ReadProbe
	opt                     Options
	jellyfinRefreshSem      chan struct{}
	jellyfinRefreshDedupeMu sync.Mutex
	jellyfinRefreshDedupe   map[string]time.Time
	selfHealMu              sync.Mutex
	lastSelfHeal            time.Time
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
	if opt.JellyfinConfig.RefreshDelaySeconds < 0 {
		opt.JellyfinConfig.RefreshDelaySeconds = 45
	}
	if opt.JellyfinConfig.TimeoutSeconds <= 0 {
		opt.JellyfinConfig.TimeoutSeconds = 20
	}
	if opt.JellyfinConfig.MaxRefreshesPerRun <= 0 {
		opt.JellyfinConfig.MaxRefreshesPerRun = 5
	}
	if opt.JellyfinConfig.RefreshDedupeMinutes <= 0 {
		opt.JellyfinConfig.RefreshDedupeMinutes = 10
	}
	if opt.SelfHealInterval <= 0 {
		opt.SelfHealInterval = 5 * time.Minute
	}
	if opt.SelfHealRecentHours <= 0 {
		opt.SelfHealRecentHours = 24
	}
	if opt.SelfHealMaxRefreshesPerRun <= 0 {
		opt.SelfHealMaxRefreshesPerRun = 5
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
	var probe, healthProbe *csi.ReadProbe
	if opt.CSI != nil {
		probe = csi.NewReadProbe(opt.CSI)
		healthProbe = csi.NewReadProbeWithLimit(opt.CSI, 4)
	}
	return &Controller{
		fileProbe:             probe,
		healthProbe:           healthProbe,
		opt:                   opt,
		jellyfinRefreshSem:    make(chan struct{}, opt.JellyfinConfig.MaxRefreshesPerRun),
		jellyfinRefreshDedupe: map[string]time.Time{},
	}
}

func (c *Controller) Run(ctx context.Context) error {
	if c.opt.FileHealth.Enabled && c.opt.RearmEnabled {
		go c.runFileHealth(ctx)
	}
	if c.opt.SelfHealEnabled {
		slog.Info("self-heal worker starting", "interval", c.opt.SelfHealInterval, "jellyfin_refresh_available", c.opt.SelfHealRefreshAvailable, "recent_hours", c.opt.SelfHealRecentHours, "max_jellyfin_refreshes_per_run", c.opt.SelfHealMaxRefreshesPerRun)
	}
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
	c.reconcileGrabSources(ctx)
	if c.opt.RearmEnabled {
		c.reconcileVisibility(ctx)
		c.reconcileRearms(ctx)
	}
	if c.opt.PruneEnabled {
		c.reconcilePrunes(ctx)
	}
	if c.opt.SelfHealEnabled {
		c.reconcileSelfHeal(ctx)
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
	if c.providerCoolingDown(ctx, "decypharr") {
		return
	}
	items, err := c.opt.Repo.PruneWorkItems(ctx, c.opt.MaxPrunesPerRun)
	if err != nil {
		slog.Error("failed to load prune work items", "error", err)
		return
	}
	c.runItems(ctx, items, c.handlePrune)
}

func (c *Controller) reconcileSelfHeal(ctx context.Context) {
	c.selfHealMu.Lock()
	if !c.lastSelfHeal.IsZero() && time.Since(c.lastSelfHeal) < c.opt.SelfHealInterval {
		c.selfHealMu.Unlock()
		return
	}
	c.lastSelfHeal = time.Now()
	c.selfHealMu.Unlock()
	c.runSelfHealPass(ctx, "scheduled")
}

func (c *Controller) RunSelfHealOnce(ctx context.Context) {
	c.runSelfHealPass(ctx, "manual")
}

// ScheduleInitialImportJellyfinRefresh queues a targeted Jellyfin refresh for newly imported
// or newly promoted media. It uses the same dedupe/cooldown path as rehydration and
// self-heal refreshes, so seed loops and webhook bursts do not repeatedly scan Jellyfin.
func (c *Controller) ScheduleInitialImportJellyfinRefresh(ctx context.Context, item model.MediaCacheState, action string) {
	if strings.TrimSpace(action) == "" {
		action = "initial_import_available"
	}
	c.scheduleJellyfinRefresh(ctx, item, action, c.opt.JellyfinConfig.RefreshAfterImport)
}

func (c *Controller) runSelfHealPass(ctx context.Context, source string) {
	if !c.opt.SelfHealRefreshAvailable {
		slog.Info("self-heal skipped", "source", source, "reason", "jellyfin_refresh_available_disabled")
		return
	}
	if !c.opt.JellyfinConfig.Enabled || c.opt.Jellyfin == nil || !c.opt.Jellyfin.Configured() || c.opt.Repo == nil {
		slog.Info("self-heal skipped", "source", source, "reason", "jellyfin_not_configured_or_repo_missing")
		return
	}

	recentSeconds := c.opt.SelfHealRecentHours * 3600
	items, err := c.opt.Repo.JellyfinSelfHealWorkItems(ctx, c.opt.Tenant, c.opt.SelfHealMaxRefreshesPerRun, recentSeconds)
	if err != nil {
		slog.Warn("self-heal Jellyfin refresh lookup failed", "source", source, "error", err)
		return
	}
	if len(items) == 0 {
		slog.Info("self-heal pass complete", "source", source, "scanned_recent_hours", c.opt.SelfHealRecentHours, "jellyfin_refresh_candidates", 0)
		return
	}
	slog.Info("self-heal queued Jellyfin refresh candidates", "source", source, "count", len(items), "recent_hours", c.opt.SelfHealRecentHours)
	for _, item := range items {
		c.scheduleJellyfinRefresh(ctx, item, "self_heal_available", true)
	}
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

	if ok, reason := itemRearmable(item); !ok {
		log.Info("rearm skipped; metadata incomplete", "reason", reason)
		_ = c.opt.Repo.MarkRequestedMetadataPending(ctx, item.ID, reason)
		payload, _ := json.Marshal(map[string]string{"reason": reason})
		_ = c.opt.Repo.Event(ctx, item.ID, "rearm_skipped_incomplete_metadata", string(payload))
		return
	}

	pathVisible := c.opt.CSI.Exists(item.SymlinkPath)
	if pathVisible && c.opt.RearmShortCircuitIfVisible && c.pathReadable(ctx, item.SymlinkPath) {
		log.Info("file already visible through CSI; marking available", "rearm_short_circuit_if_visible", true)
		cachedUntil := time.Now().Add(c.opt.CacheGrace)
		_ = c.opt.Repo.MarkAvailable(ctx, item.ID, valueOrEmpty(item.InfoHash), cachedUntil)
		return
	}

	if pathVisible {
		log.Info("CSI path is visible but rearm will still be queued because CSI visibility is not authoritative for archived cache state")
	} else {
		log.Info("file missing; rearming", "backend", "decypharr")
	}
	if err := c.opt.Repo.MarkRearming(ctx, item.ID); err != nil {
		log.Error("failed to mark rearming", "error", err)
		return
	}

	torrent, err := c.resolveRearmTorrent(ctx, item)
	if err != nil {
		if errors.Is(err, model.ErrSourceRecoveryRequired) {
			_ = c.opt.Repo.MarkSourceRecoveryRequired(ctx, item.ID)
			_ = c.opt.Repo.Event(ctx, item.ID, "source_recovery_required", `{}`)
			log.Warn("rearm blocked; original source must be recovered", "error", err)
			return
		}

		if isMissingHistoryError(err) {
			reason := "missing stored metadata and Arr grabbed/import history; keeping item REQUESTED until metadata is available"
			log.Info("rearm skipped; missing Arr history metadata", "reason", reason, "error", err)
			_ = c.opt.Repo.MarkRequestedMetadataPending(ctx, item.ID, reason)
			payload, _ := json.Marshal(map[string]string{"reason": reason, "error": err.Error()})
			_ = c.opt.Repo.Event(ctx, item.ID, "rearm_skipped_missing_arr_history", string(payload))
			return
		}
		c.fail(ctx, item, err)
		return
	}

	category := c.categoryFor(item.MediaType)
	if err := c.opt.Repo.SaveTorrentMetadata(ctx, item.ID, torrent, category); err != nil {
		c.fail(ctx, item, err)
		return
	}

	metaJSON, _ := json.Marshal(map[string]string{"infohash": torrent.InfoHash, "protocol": torrent.Protocol, "original_download_id": torrent.DownloadID, "source": torrent.Source, "source_kind": string(torrent.Kind), "source_title": torrent.SourceTitle, "client": "decypharr", "category": category})
	_ = c.opt.Repo.Event(ctx, item.ID, "torrent_metadata_resolved", string(metaJSON))
	item.SourceProtocol = torrent.Protocol
	hash := torrent.InfoHash
	if torrent.Protocol == model.ProtocolUsenet {
		hash, err = c.opt.Repo.EnsureNZBJob(ctx, item.ID, func(job string) (string, error) {
			_, found, statusErr := c.opt.Decypharr.NZBStatus(ctx, job, category)
			if statusErr != nil {
				return "", statusErr
			}
			if found {
				return job, nil
			}
			exists, err := c.opt.Decypharr.LibraryHasDownload(ctx, job)
			if err != nil {
				return "", err
			}
			if exists {
				return job, nil
			}
			return c.opt.Decypharr.AddNZB(ctx, torrent, category)
		})
		if err != nil {
			c.waitAfterStatusError(ctx, item, torrent.DownloadID, err)
			return
		}

		item.SourceProtocol = model.ProtocolUsenet
		item.DownloadJobID = hash
	} else {
		addResult, err := c.opt.Decypharr.AddTorrent(ctx, torrent, category)
		if err != nil {
			c.failProvider(ctx, item, "decypharr", err)
			return
		}
		hash = firstNonEmpty(addResult.Hash, hash)
	}

	payload, _ := json.Marshal(map[string]string{"infohash": torrent.InfoHash, "protocol": torrent.Protocol, "download_job_id": item.DownloadJobID, "source_kind": string(torrent.Kind), "category": category, "client": "decypharr"})
	_ = c.opt.Repo.Event(ctx, item.ID, "decypharr_readd_requested", string(payload))

	ready, err := c.rearmReady(ctx, item, hash)
	if err != nil {
		c.waitAfterStatusError(ctx, item, hash, err)
		return
	}

	if !ready {
		c.markWaitingVisibility(ctx, item, hash, "waiting for Decypharr download completion before mount visibility")
		return
	}

	if ok, reason := itemHasUsablePath(item); !ok {
		log.Info("visibility check skipped; metadata incomplete", "reason", reason)
		_ = c.opt.Repo.MarkRequestedMetadataPending(ctx, item.ID, reason)
		payload, _ := json.Marshal(map[string]string{"reason": reason})
		_ = c.opt.Repo.Event(ctx, item.ID, "visibility_skipped_incomplete_metadata", string(payload))
		return
	}
	c.refreshRclone(ctx, item.SymlinkPath)

	if ok := c.waitForCSI(ctx, item.SymlinkPath); !ok {
		c.markWaitingVisibility(ctx, item, hash, fmt.Sprintf("CSI path did not appear within %s after provider add; waiting for mount visibility", c.opt.CSIVisibilityTimeout))
		return
	}

	cachedUntil := time.Now().Add(c.opt.CacheGrace)
	if err := c.opt.Repo.MarkAvailable(ctx, item.ID, hash, cachedUntil); err != nil {
		log.Error("failed to mark available", "error", err)
		return
	}
	c.scheduleJellyfinRefresh(ctx, item, "rehydration_complete", c.opt.JellyfinConfig.RefreshAfterRearm)

	finalJSON, _ := json.Marshal(map[string]string{
		"cached_until": cachedUntil.Format(time.RFC3339),
		"infohash":     torrent.InfoHash,
		"protocol":     torrent.Protocol, "download_job_id": item.DownloadJobID,
		"client": "decypharr",
	})
	_ = c.opt.Repo.Event(ctx, item.ID, "available", string(finalJSON))
	log.Info("rehydration complete", "cached_until", cachedUntil, "protocol", torrent.Protocol, "download_identity", hash)
}

func (c *Controller) handlePrune(ctx context.Context, item model.MediaCacheState) {
	log := slog.With(
		"tenant", item.Tenant,
		"media_type", item.MediaType,
		"arr_id", item.ArrID,
		"infohash", valueOrEmpty(item.InfoHash),
		"download_client", valueOrEmpty(item.DownloadClient),
	)

	if item.SourceProtocol == model.ProtocolUsenet {
		log.Info("Usenet automatic pruning is disabled")
		if c.opt.Repo != nil {
			_ = c.opt.Repo.MarkAvailable(ctx, item.ID, "", time.Now().Add(c.opt.CacheGrace))
		}
		return
	}
	infoHash := valueOrEmpty(item.InfoHash)

	if c.opt.Decypharr == nil || !c.opt.Decypharr.Configured() {
		c.fail(ctx, item, fmt.Errorf("cannot prune: missing Decypharr client"))
		return
	}
	// Persist full metadata before clearing the queue or provider placement.
	if err := c.captureRearmMetadata(ctx, item); err != nil {
		if errors.Is(err, model.ErrSourceRecoveryRequired) {
			_ = c.opt.Repo.MarkSourceRecoveryRequired(ctx, item.ID)
			log.Warn("prune blocked; original source must be recovered", "error", err)
			return
		}

		c.fail(ctx, item, err)
		return
	}
	refreshed, found, err := c.opt.Repo.GetState(ctx, item.Tenant, item.MediaType, item.ArrID)
	if err != nil {
		c.fail(ctx, item, err)
		return
	}
	if !found || (refreshed.SourceProtocol != model.ProtocolUsenet && valueOrEmpty(refreshed.InfoHash) == "") {
		c.fail(ctx, item, fmt.Errorf("cannot prune: missing current release identity"))
		return
	}
	item = refreshed
	if item.SourceProtocol == model.ProtocolUsenet {
		log.Info("Usenet automatic pruning is disabled")
		if c.opt.Repo != nil {
			_ = c.opt.Repo.MarkAvailable(ctx, item.ID, "", time.Now().Add(c.opt.CacheGrace))
		}
		return
	}
	infoHash = valueOrEmpty(item.InfoHash)

	if err := c.opt.Repo.MarkPruning(ctx, item.ID); err != nil {
		log.Error("failed to mark pruning", "error", err)
		return
	}
	if err := c.opt.Decypharr.PruneTorrent(ctx, infoHash); err != nil {
		c.failProvider(ctx, item, "decypharr", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"infohash": infoHash, "client": "decypharr"})
	_ = c.opt.Repo.Event(ctx, item.ID, "decypharr_prune_requested", string(payload))

	if c.opt.PruneWaitForCSIGone {
		if ok := c.waitForCSIGone(ctx, item.SymlinkPath); !ok {
			log.Warn("Decypharr accepted prune but CSI path is still visible", "wait", c.opt.CSIWait)
			_ = c.opt.Repo.Event(ctx, item.ID, "csi_path_still_visible_after_prune", `{}`)
		}
	} else if c.opt.CSI.Exists(item.SymlinkPath) {
		log.Info("CSI path still visible after Decypharr prune request; prune_wait_for_csi_gone=false")
		_ = c.opt.Repo.Event(ctx, item.ID, "csi_path_visible_ignored_after_prune", `{}`)
	}

	if err := c.opt.Repo.MarkArchived(ctx, item.ID); err != nil {
		log.Error("failed to mark archived", "error", err)
		return
	}
	c.scheduleJellyfinRefresh(ctx, item, "prune_archived", c.opt.JellyfinConfig.RefreshAfterPrune)

	_ = c.opt.Repo.Event(ctx, item.ID, "archived", "{}")
	log.Info("Decypharr accepted prune; item archived", "infohash", infoHash)
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
	if ok, reason := itemHasUsablePath(item); !ok {
		log.Info("visibility check skipped; metadata incomplete", "reason", reason)
		_ = c.opt.Repo.MarkRequestedMetadataPending(ctx, item.ID, reason)
		payload, _ := json.Marshal(map[string]string{"reason": reason})
		_ = c.opt.Repo.Event(ctx, item.ID, "visibility_skipped_incomplete_metadata", string(payload))
		return
	}

	if c.providerCoolingDown(ctx, "decypharr") {
		return
	}
	ready, err := c.rearmReady(ctx, item, valueOrEmpty(item.InfoHash))
	if err != nil {
		c.waitAfterStatusError(ctx, item, valueOrEmpty(item.InfoHash), err)
		return
	}

	if !ready {
		c.markWaitingVisibility(ctx, item, valueOrEmpty(item.InfoHash), "waiting for Decypharr download completion before mount visibility")
		return
	}

	c.refreshRclone(ctx, item.SymlinkPath)
	if !c.pathReadable(ctx, item.SymlinkPath) {
		msg := "waiting for CSI/rclone mount visibility after successful provider add"
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
	c.scheduleJellyfinRefresh(ctx, item, "visibility_available", c.opt.JellyfinConfig.RefreshAfterVisibility)
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
	log.Warn("rearm accepted; waiting for provider readiness or mount visibility", "next_retry_at", next, "infohash", infoHash)
}

func (c *Controller) scheduleJellyfinRefresh(ctx context.Context, item model.MediaCacheState, action string, enabled bool) {
	if !enabled {
		slog.Debug("Jellyfin refresh skipped; action disabled", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action)
		return
	}
	if !c.opt.JellyfinConfig.Enabled {
		slog.Info("Jellyfin refresh skipped; jellyfin.enabled=false", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action)
		return
	}
	if c.opt.Jellyfin == nil || !c.opt.Jellyfin.Configured() {
		slog.Warn("Jellyfin refresh skipped; Jellyfin client is not configured", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action)
		return
	}
	if strings.TrimSpace(item.SymlinkPath) == "" {
		slog.Warn("Jellyfin refresh skipped; missing symlink path", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action)
		return
	}
	targetPath := item.SymlinkPath
	reserved, reason := c.reserveJellyfinRefresh(ctx, item, action, targetPath)
	if !reserved {
		slog.Info("Jellyfin refresh skipped by dedupe", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action, "target_path", targetPath, "reason", reason, "dedupe", c.opt.JellyfinConfig.RefreshDedupeMinutes)
		if c.opt.Repo != nil {
			_ = c.opt.Repo.RecordJellyfinRefresh(context.Background(), item.Tenant, item.ID, item.ArrID, action, jellyfinRefreshScope(item.MediaType), targetPath, "skipped", "deduped: "+reason)
		}
		return
	}
	slog.Info("Jellyfin refresh queued", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action, "target_path", targetPath, "delay", c.opt.JellyfinConfig.RefreshDelaySeconds, "dedupe", c.opt.JellyfinConfig.RefreshDedupeMinutes)
	go func() {
		timer := time.NewTimer(time.Duration(c.opt.JellyfinConfig.RefreshDelaySeconds) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		select {
		case c.jellyfinRefreshSem <- struct{}{}:
			defer func() { <-c.jellyfinRefreshSem }()
		default:
			slog.Warn("Jellyfin refresh skipped; refresh limit is full", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action, "target_path", targetPath)
			if c.opt.Repo != nil {
				_ = c.opt.Repo.RecordJellyfinRefresh(context.Background(), item.Tenant, item.ID, item.ArrID, action, jellyfinRefreshScope(item.MediaType), targetPath, "skipped", "jellyfin refresh limit full")
			}
			return
		}
		if item.SourceProtocol == model.ProtocolUsenet && c.opt.CSI != nil && !c.opt.CSI.Readable(targetPath) {
			slog.Warn("Jellyfin refresh skipped; Usenet file is not readable yet", "arr_id", item.ArrID, "target_path", targetPath)
			if c.opt.Repo != nil {
				_ = c.opt.Repo.RecordJellyfinRefresh(ctx, item.Tenant, item.ID, item.ArrID, action, jellyfinRefreshScope(item.MediaType), targetPath, "skipped", "Usenet file not readable; self-heal may retry")
			}
			return
		}
		refreshCtx, cancel := context.WithTimeout(ctx, time.Duration(c.opt.JellyfinConfig.TimeoutSeconds)*time.Second)
		defer cancel()
		slog.Info("Jellyfin refresh starting", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action, "target_path", targetPath)
		err := c.opt.Jellyfin.RefreshPath(refreshCtx, targetPath)
		status := "success"
		message := ""
		if err != nil {
			status = "failed"
			message = err.Error()
			slog.Warn("Jellyfin refresh failed", "tenant", item.Tenant, "arr_id", item.ArrID, "target_path", targetPath, "action", action, "error", err)
		} else {
			slog.Info("Jellyfin refresh request accepted", "tenant", item.Tenant, "arr_id", item.ArrID, "target_path", targetPath, "action", action)
		}
		if c.opt.Repo != nil {
			_ = c.opt.Repo.RecordJellyfinRefresh(refreshCtx, item.Tenant, item.ID, item.ArrID, action, jellyfinRefreshScope(item.MediaType), targetPath, status, message)
		}
	}()
}

func (c *Controller) reserveJellyfinRefresh(ctx context.Context, item model.MediaCacheState, action string, targetPath string) (bool, string) {
	if c.opt.JellyfinConfig.RefreshDedupeMinutes <= 0 {
		return true, ""
	}
	now := time.Now()
	key := strings.Join([]string{item.Tenant, string(item.MediaType), targetPath}, "|")

	c.jellyfinRefreshDedupeMu.Lock()
	for k, until := range c.jellyfinRefreshDedupe {
		if now.After(until) {
			delete(c.jellyfinRefreshDedupe, k)
		}
	}
	if until, ok := c.jellyfinRefreshDedupe[key]; ok && now.Before(until) {
		c.jellyfinRefreshDedupeMu.Unlock()
		return false, "refresh already queued or recently attempted until " + until.Format(time.RFC3339)
	}
	c.jellyfinRefreshDedupe[key] = now.Add(time.Duration(c.opt.JellyfinConfig.RefreshDedupeMinutes) * time.Minute)
	c.jellyfinRefreshDedupeMu.Unlock()

	if c.opt.Repo != nil {
		cutoff := now.Add(-time.Duration(c.opt.JellyfinConfig.RefreshDedupeMinutes) * time.Minute)
		recent, err := c.opt.Repo.RecentSuccessfulJellyfinRefresh(ctx, item.Tenant, targetPath, cutoff)
		if err != nil {
			slog.Warn("Jellyfin refresh dedupe DB check failed; continuing", "tenant", item.Tenant, "arr_id", item.ArrID, "action", action, "target_path", targetPath, "cutoff", cutoff, "error", err)
			return true, ""
		}
		if recent {
			c.jellyfinRefreshDedupeMu.Lock()
			delete(c.jellyfinRefreshDedupe, key)
			c.jellyfinRefreshDedupeMu.Unlock()
			return false, "successful refresh already recorded within dedupe window"
		}
	}

	return true, ""
}

func jellyfinRefreshScope(mediaType model.MediaType) string {
	switch mediaType {
	case model.MediaSeries:
		return "series_file"
	case model.MediaMovie:
		return "movie_file"
	default:
		return "media_file"
	}
}

func (c *Controller) refreshRclone(ctx context.Context, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
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
	if strings.TrimSpace(path) == "" {
		return false
	}
	deadline := time.Now().Add(c.opt.CSIVisibilityTimeout)
	for time.Now().Before(deadline) {
		if c.pathReadable(ctx, path) {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(c.opt.CSIVisibilityPoll):
		}
	}
	return c.pathReadable(ctx, path)
}

func (c *Controller) waitForCSIGone(ctx context.Context, path string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}
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

func itemHasUsablePath(item model.MediaCacheState) (bool, string) {
	if item.ArrID <= 0 {
		return false, "placeholder or invalid arr_id"
	}
	if strings.TrimSpace(item.SymlinkPath) == "" {
		return false, "missing symlink_path"
	}
	if item.MediaType == model.MediaSeries {
		if item.SeriesID == nil || *item.SeriesID <= 0 {
			return false, "series row missing series_id"
		}
		if item.EpisodeFileID == nil || *item.EpisodeFileID <= 0 {
			return false, "series row missing episode_file_id"
		}
	}
	return true, ""
}

func itemRearmable(item model.MediaCacheState) (bool, string) {
	if item.State == model.StateRequested {
		return false, "REQUESTED rows are pending import and are not rearmable"
	}
	return itemHasUsablePath(item)
}

func isMissingHistoryError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no matching movie history records") || strings.Contains(msg, "no matching series history records") || strings.Contains(msg, "no matching") && strings.Contains(msg, "history")
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

func (c *Controller) waitAfterStatusError(ctx context.Context, item model.MediaCacheState, hash string, err error) {
	if errors.Is(err, model.ErrSourceRecoveryRequired) {
		_ = c.opt.Repo.MarkSourceRecoveryRequired(ctx, item.ID, err.Error())
		slog.Warn("download recovery required; automatic resubmission stopped", "arr_id", item.ArrID, "error", err)
		return
	}
	if isRateLimited(err) {
		_ = c.opt.Repo.SetProviderCooldown(ctx, item.Tenant, "decypharr", c.opt.ProviderCooldown, err.Error())
	}
	c.markWaitingVisibility(ctx, item, hash, "Decypharr status check failed; retrying without resubmission: "+err.Error())
}
