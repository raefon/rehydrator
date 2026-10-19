package controller

import (
	"context"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) rearmReady(ctx context.Context, item model.MediaCacheState, hash string) (bool, error) {
	info, err := c.opt.Decypharr.TorrentInfo(ctx, hash)
	if err != nil {
		return false, err
	}
	if info != nil {
		return info.Ready(), nil
	}
	// Arr may already have removed the completed queue entry. Require an exact
	// library identity and a real read so stale CSI metadata cannot complete rearm.
	exists, err := c.opt.Decypharr.LibraryHasTorrent(ctx, hash)
	if err != nil || !exists {
		return false, err
	}
	c.refreshRclone(ctx, item.SymlinkPath)
	return c.opt.CSI.Readable(item.SymlinkPath), nil
}
