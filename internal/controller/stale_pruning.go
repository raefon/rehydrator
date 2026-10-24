package controller

import (
	"context"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

// ReconcileDisabledPrune is called only for an imported file observed in Arr's
// current inventory. It hands abandoned pruning to the bounded provider sweep.
func (c *Controller) ReconcileDisabledPrune(ctx context.Context, item model.MediaCacheState) {
	if c.opt.PruneEnabled || !c.opt.RearmEnabled || !c.opt.FileHealth.Enabled ||
		!c.opt.TorBoxHealth.Enabled || c.opt.Repo == nil || item.State != model.StatePruning {
		return
	}
	changed, err := c.opt.Repo.ReleaseStalePruning(ctx, item.Tenant, item.ID, item.SymlinkPath, 15*time.Minute)
	if err != nil {
		slog.Warn("stale pruning reconciliation failed", "tenant", item.Tenant, "arr_id", item.ArrID, "error", err)
	} else if changed {
		slog.Info("stale pruning returned to provider health checks", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "path", item.SymlinkPath)
	}
}
