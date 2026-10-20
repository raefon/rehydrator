package controller

import (
	"context"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) rearmReady(ctx context.Context, item model.MediaCacheState, hash string) (bool, error) {
	if item.SourceProtocol == model.ProtocolUsenet {
		job := item.DownloadJobID
		if job == "" {
			job = hash
		}
		ready, found, err := c.opt.Decypharr.NZBStatus(ctx, job, c.categoryFor(item.MediaType))
		if err != nil {
			return false, err
		}
		if found {
			return ready && c.opt.CSI.Readable(item.SymlinkPath), nil
		}
		exists, err := c.opt.Decypharr.LibraryHasDownload(ctx, job)
		if err != nil || !exists {
			return false, err
		}
		c.refreshRclone(ctx, item.SymlinkPath)
		return c.opt.CSI.Readable(item.SymlinkPath), nil
	}
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
