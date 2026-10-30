package controller

import (
	"context"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"log/slog"
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
		claimed, err := c.opt.Repo.ClaimJellyfinInventory(ctx, item.Tenant, item.ID, target, time.Duration(cfg.CooldownMinutes)*time.Minute)
		if err != nil {
			slog.Warn("Jellyfin inventory scan reservation failed", "error", err)
			continue
		}
		if !claimed {
			continue
		}
		attempted++
		// Deliberately no filesystem reads here, including for NNTP-backed imports.
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
	slog.Info("Jellyfin inventory sync complete", "tracked", len(items), "indexed_paths", len(paths), "missing", missing, "attempted", attempted, "accepted", accepted, "failed", failed)
}
