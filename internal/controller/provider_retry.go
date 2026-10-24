package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/model"
)

// Unknown queue states are treated as active; never delete a downloading job.
func providerRetryQueueIdle(info *decypharr.TorrentInfo) bool {
	if info == nil {
		return true
	}
	switch strings.ToLower(info.State) {
	case "uploading", "stalledup", "pausedup", "stoppedup", "queuedup", "error", "missingfiles":
		return true
	default:
		return false
	}
}

func (c *Controller) retryMissingProvider(ctx context.Context, item model.MediaCacheState) {
	if !c.opt.TorBoxHealth.Enabled || item.SourceProtocol == model.ProtocolUsenet || c.opt.Repo == nil {
		return
	}
	hash := valueOrEmpty(item.InfoHash)
	ready, err := c.torboxReady(ctx, hash)
	if err != nil || ready {
		return
	}
	info, err := c.opt.Decypharr.TorrentInfo(ctx, hash)
	if err != nil || !providerRetryQueueIdle(info) {
		return
	}
	// Use only the exact saved source; retries never re-search Arr or Prowlarr.
	source, found, err := c.opt.Repo.SavedTorrentSource(ctx, item.ID)
	if err != nil || !found || !strings.EqualFold(source.InfoHash, hash) || source.Protocol == model.ProtocolUsenet {
		return
	}
	source, err = source.ValidateSource()
	if err != nil {
		return
	}
	claimed, err := c.opt.Repo.ClaimProviderRetry(ctx, item.Tenant, item.ID, hash, item.SymlinkPath)
	if err != nil {
		slog.Warn("provider recovery retry reservation failed", "infohash", hash, "error", err)
		return
	}
	if !claimed {
		return
	}
	// Recheck after claiming, in case completion raced the reservation.
	c.providerReadyMu.Lock()
	delete(c.providerReadyCache, hash)
	c.providerReadyMu.Unlock()
	ready, err = c.torboxReady(ctx, hash)
	if err == nil && !ready {
		info, err = c.opt.Decypharr.TorrentInfo(ctx, hash)
		if err == nil && !providerRetryQueueIdle(info) {
			err = fmt.Errorf("Decypharr job became active; retry skipped")
		}
		if err == nil && info != nil {
			err = c.opt.Decypharr.DeleteTorrent(ctx, hash, false)
		}
		if err == nil {
			_, err = c.opt.Decypharr.AddTorrent(ctx, source, c.categoryFor(item.MediaType))
		}
	} else if err == nil {
		err = fmt.Errorf("provider placement appeared; retry skipped")
	}
	detail := "Decypharr retry accepted; awaiting provider readiness"
	if err != nil {
		detail = err.Error()
	}
	_ = c.opt.Repo.RecordProviderRetry(ctx, item.Tenant, hash, detail)
	if err != nil {
		if isRateLimited(err) {
			_ = c.opt.Repo.SetProviderCooldown(ctx, item.Tenant, "decypharr", c.opt.ProviderCooldown, "provider recovery retry rate limit")
		}
		slog.Warn("provider recovery retry deferred or failed", "tenant", item.Tenant, "infohash", hash, "error", err)
	} else {
		slog.Info("missing provider release resubmitted through Decypharr", "tenant", item.Tenant, "infohash", hash)
	}
}
