package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) resolveRearmTorrent(ctx context.Context, item model.MediaCacheState) (model.TorrentMetadata, error) {
	if c.opt.Repo != nil {
		if err := c.opt.Repo.LinkGrabSourceForItem(ctx, item.Tenant, item.ID); err != nil {
			return model.TorrentMetadata{}, err
		}
		pending, err := c.opt.Repo.CurrentGrabSourcePending(ctx, item.ID)
		if err != nil {
			return model.TorrentMetadata{}, err
		}
		if pending {
			return model.TorrentMetadata{}, fmt.Errorf("source capture pending for current import")
		}
		if refreshed, found, err := c.opt.Repo.GetState(ctx, item.Tenant, item.MediaType, item.ArrID); err != nil {
			return model.TorrentMetadata{}, err
		} else if found {
			item = refreshed
		}
		saved, found, err := c.opt.Repo.SavedTorrentSource(ctx, item.ID)
		if err != nil {
			return saved, err
		}
		if found && saved.Kind != model.SourceRecoveryRequired && strings.EqualFold(saved.InfoHash, strings.TrimSpace(valueOrEmpty(item.InfoHash))) {
			saved.SourceTitle = valueOrEmpty(item.SourceTitle)
			return saved.ValidateSource()
		}
	}
	// Legacy hash/magnet columns do not establish source provenance. Recover the
	// original source from Arr instead of guessing how this release was submitted.
	client, err := c.arrClientFor(item.MediaType)
	if err != nil {
		return model.TorrentMetadata{}, fmt.Errorf("%w: %v", model.ErrSourceRecoveryRequired, err)
	}
	if !client.Configured() {
		return model.TorrentMetadata{}, model.ErrSourceRecoveryRequired
	}
	torrent, err := client.LatestGrabbedTorrentForState(ctx, item)
	if err != nil {
		return torrent, fmt.Errorf("%w: %v", model.ErrSourceRecoveryRequired, err)
	}
	if c.opt.Decypharr == nil {
		return torrent, model.ErrSourceRecoveryRequired
	}
	recovered, err := c.opt.Decypharr.PrepareTorrent(ctx, torrent)
	if err != nil {
		return recovered, err
	}
	if known := strings.TrimSpace(valueOrEmpty(item.InfoHash)); known != "" && !strings.EqualFold(known, recovered.InfoHash) {
		return recovered, fmt.Errorf("%w: Arr returned a different release identity", model.ErrSourceRecoveryRequired)
	}
	return recovered, nil
}

// Preserve the original submission source before removing its provider placement.
func (c *Controller) captureRearmMetadata(ctx context.Context, item model.MediaCacheState) error {
	torrent, err := c.resolveRearmTorrent(ctx, item)
	if err != nil {
		return err
	}
	return c.opt.Repo.SaveTorrentMetadata(ctx, item.ID, torrent, c.categoryFor(item.MediaType))
}
