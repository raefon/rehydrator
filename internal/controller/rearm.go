package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) resolveRearmTorrent(ctx context.Context, item model.MediaCacheState) (model.TorrentMetadata, error) {
	if c.directGateway() {
		id := valueOrEmpty(item.InfoHash)
		if item.SourceProtocol == model.ProtocolUsenet {
			id = item.SourceDownloadID
		}
		if id != "" {
			source, err := c.opt.Gateway.Source(ctx, id)
			if err == nil {
				return source, nil
			}
		}
	}
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
		if found && saved.Kind != model.SourceRecoveryRequired && ((saved.Protocol == model.ProtocolUsenet && item.SourceProtocol == model.ProtocolUsenet) || (saved.Protocol != model.ProtocolUsenet && strings.EqualFold(saved.InfoHash, strings.TrimSpace(valueOrEmpty(item.InfoHash))))) {
			saved.SourceTitle = valueOrEmpty(item.SourceTitle)
			return saved.ValidateSource()
		}
	}
	return model.TorrentMetadata{}, model.ErrSourceRecoveryRequired
}
