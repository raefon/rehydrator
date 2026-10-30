package controller

import (
	"context"
	"log/slog"
	"time"
)

func (c *Controller) reconcileArrMissing(ctx context.Context) {
	if !c.opt.JellyfinConfig.InventorySync.MarkMissingInArr || c.opt.CSI == nil {
		return
	}
	// A readable canary on the same remote mount protects against treating
	// a mount outage as many individual missing releases.
	if c.opt.FileHealth.CanaryPath == "" || c.healthProbe == nil {
		slog.Warn("Arr missing-file updates require a same-mount file_health.canary_path")
		return
	}
	canaryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	healthy, canaryErr := c.healthProbe.Check(canaryCtx, c.opt.FileHealth.CanaryPath)
	cancel()
	if canaryErr != nil || !healthy {
		slog.Warn("Arr missing-file updates paused; canary unreadable", "error", canaryErr)
		return
	}
	items, err := c.opt.Repo.SourceRecoveryWorkItems(ctx, c.opt.Tenant, c.opt.JellyfinConfig.InventorySync.MaxRefreshesPerRun)
	if err != nil {
		slog.Warn("Arr missing-file lookup failed", "error", err)
		return
	}
	for _, item := range items {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		missing, err := c.arrMissingProbe.Check(probeCtx, item.SymlinkPath)
		cancel()
		if err != nil || !missing {
			continue
		}
		client, err := c.arrClientFor(item.MediaType)
		if err == nil {
			err = client.RemoveMissingImportedFile(ctx, item, func(checkCtx context.Context, path string) (bool, error) {
				bounded, cancel := context.WithTimeout(checkCtx, 5*time.Second)
				defer cancel()
				return c.arrMissingProbe.Check(bounded, path)
			})
		}
		if err != nil {
			slog.Warn("Arr missing-file update failed", "arr_id", item.ArrID, "error", err)
			continue
		}
		_ = c.opt.Repo.Event(ctx, item.ID, "arr_missing_reported", `{"source":"confirmed_dangling_symlink"}`)
		slog.Info("Arr imported file removed after confirmed missing target", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "path", item.SymlinkPath)
	}
}
