package controller

import (
	"context"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

// ReconcileImportedRecovery is called only for an imported file observed in Arr's
// current inventory. It hands abandoned pruning and opted-in archived imports
// to the bounded provider sweep.
func (c *Controller) ReconcileImportedRecovery(ctx context.Context, item model.MediaCacheState) {
	if c.opt.PruneEnabled || !c.opt.RearmEnabled || !c.opt.FileHealth.Enabled ||
		!c.opt.TorBoxHealth.Enabled || c.opt.Repo == nil {
		return
	}
	if item.State == model.StateArchived && c.opt.FileHealth.RecoverArchived && !item.RearmRequested && item.SourceProtocol == model.ProtocolTorrent {
		source, found, err := c.opt.Repo.SavedTorrentSource(ctx, item.ID)
		if err != nil {
			slog.Warn("archived recovery source lookup failed", "tenant", item.Tenant, "arr_id", item.ArrID, "error", err)
			return
		}
		if !found {
			return
		}
		source, err = source.ValidateSource()
		if err != nil || source.Protocol != model.ProtocolTorrent || source.InfoHash != valueOrEmpty(item.InfoHash) {
			return
		}
		changed, err := c.opt.Repo.ReleaseArchivedForHealth(ctx, item.Tenant, item.ID, item.SymlinkPath, source.InfoHash)
		if err != nil {
			slog.Warn("archived recovery reconciliation failed", "tenant", item.Tenant, "arr_id", item.ArrID, "error", err)
		} else if changed {
			slog.Info("archived import returned to provider health checks", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "infohash", source.InfoHash)
		}
		return
	}
	if item.State != model.StatePruning {
		return
	}
	changed, err := c.opt.Repo.ReleaseStalePruning(ctx, item.Tenant, item.ID, item.SymlinkPath, 15*time.Minute)
	if err != nil {
		slog.Warn("stale pruning reconciliation failed", "tenant", item.Tenant, "arr_id", item.ArrID, "error", err)
	} else if changed {
		slog.Info("stale pruning returned to provider health checks", "tenant", item.Tenant, "media_type", item.MediaType, "arr_id", item.ArrID, "path", item.SymlinkPath)
	}
}
