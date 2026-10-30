package controller

import (
	"context"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"github.com/raefon/rehydrator/internal/model"
	"log/slog"
	"strings"
	"time"
)

func (c *Controller) runJellyfinInventory(ctx context.Context) {
	cfg := c.opt.JellyfinConfig.InventorySync
	if !cfg.Enabled || !c.opt.JellyfinConfig.Enabled || c.opt.Jellyfin == nil || !c.opt.Jellyfin.Configured() || c.opt.Repo == nil {
		return
	}
	if c.opt.JellyfinConfig.RefreshMode != "targeted_scan" {
		slog.Warn("Jellyfin inventory sync requires targeted_scan mode")
		return
	}
	slog.Info("Jellyfin inventory sync starting", "interval_seconds", cfg.IntervalSeconds, "max_refreshes", cfg.MaxRefreshesPerRun)
	ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		c.reconcileJellyfinInventory(passCtx)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (c *Controller) reconcileJellyfinInventory(ctx context.Context) {
	cfg := c.opt.JellyfinConfig.InventorySync
	paths, err := c.opt.Jellyfin.InventoryPaths(ctx)
	if err != nil {
		slog.Warn("Jellyfin inventory sync aborted; inventory inconclusive", "error", err)
		return
	}
	items, err := c.opt.Repo.ImportedInventory(ctx, c.opt.Tenant)
	if err != nil {
		slog.Warn("Jellyfin inventory lookup failed", "error", err)
		return
	}
	missing, attempted, accepted, failed := 0, 0, 0, 0
	sourceRequired, metadataChecked := 0, 0
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		target := jellyfin.MapInventoryPath(item.SymlinkPath, cfg.PathMappings)
		if target == "" || paths[target] {
			continue
		}
		missing++
		if attempted >= cfg.MaxRefreshesPerRun {
			continue
		}
		// Only source-less torrents need this metadata-only check. Never stat
		// NNTP-backed paths or use a missing Jellyfin entry as proof of loss.
		if item.SourceProtocol == model.ProtocolTorrent && c.opt.CSI != nil {
			saved, found, err := c.opt.Repo.SavedTorrentSource(ctx, item.ID)
			if err != nil {
				slog.Warn("Jellyfin inventory source lookup failed", "arr_id", item.ArrID, "error", err)
				continue
			}
			validSource := false
			if found {
				validated, validationErr := saved.ValidateSource()
				validSource = validationErr == nil && validated.Protocol == model.ProtocolTorrent && strings.EqualFold(validated.InfoHash, valueOrEmpty(item.InfoHash))
			}
			if !validSource {
				if metadataChecked >= cfg.MaxRefreshesPerRun {
					continue
				}
				metadataChecked++
				probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				missingPath, probeErr := c.missingPathProbe.Check(probeCtx, item.SymlinkPath)
				cancel()
				if probeErr != nil {
					slog.Warn("Jellyfin inventory path metadata inconclusive", "arr_id", item.ArrID, "error", probeErr)
					continue
				}
				if missingPath {
					changed, err := c.opt.Repo.FlagMissingInventorySource(ctx, item.Tenant, item.ID, item.SymlinkPath)
					if err != nil {
						slog.Warn("Jellyfin inventory source recovery flag failed", "arr_id", item.ArrID, "error", err)
						continue
					}
					if changed {
						sourceRequired++
						_ = c.opt.Repo.RecordJellyfinRefresh(ctx, item.Tenant, item.ID, item.ArrID, "inventory_source_recovery_required", jellyfinRefreshScope(item.MediaType), target, "skipped", "missing torrent path has no matching saved source")
						slog.Warn("Jellyfin inventory item requires source recovery", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "path", item.SymlinkPath)
					}
					continue
				}
			}
		}
		claimed, err := c.opt.Repo.ClaimJellyfinInventory(ctx, item.Tenant, item.ID, target, time.Duration(cfg.CooldownMinutes)*time.Minute)
		if err != nil {
			slog.Warn("Jellyfin inventory scan reservation failed", "error", err)
			continue
		}
		if !claimed {
			continue
		}
		attempted++
		// No file-content reads; NNTP-backed imports bypass the metadata check.
		err = c.opt.Jellyfin.RefreshPath(ctx, target)
		status, detail := "success", ""
		if err != nil {
			failed++
			status = "failed"
			detail = err.Error()
		} else {
			accepted++
		}
		_ = c.opt.Repo.RecordJellyfinRefresh(ctx, item.Tenant, item.ID, item.ArrID, "inventory_missing", jellyfinRefreshScope(item.MediaType), target, status, detail)
		slog.Info("Jellyfin inventory targeted scan", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "target_path", target, "status", status, "error", err)
	}
	slog.Info("Jellyfin inventory sync complete", "tracked", len(items), "indexed_paths", len(paths), "missing", missing, "attempted", attempted, "accepted", accepted, "failed", failed, "source_recovery_required", sourceRequired, "metadata_checked", metadataChecked)
}
