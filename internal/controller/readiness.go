package controller

import (
	"context"
	"time"

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
			return ready && c.pathReadable(ctx, item.SymlinkPath), nil
		}
		exists, err := c.opt.Decypharr.LibraryHasDownload(ctx, job)
		if err != nil || !exists {
			return false, err
		}
		c.refreshRclone(ctx, item.SymlinkPath)
		return c.pathReadable(ctx, item.SymlinkPath), nil
	}
	if c.opt.TorBoxHealth.Enabled {
		ready, err := c.torboxReady(ctx, hash)
		if err != nil || !ready {
			return false, err
		}
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
	return c.pathReadable(ctx, item.SymlinkPath), nil
}

// Share the bounded read slot so a stuck mount cannot accumulate rearm probes.
func (c *Controller) pathReadable(ctx context.Context, path string) bool {
	if c.fileProbe == nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	readable, err := c.fileProbe.Check(probeCtx, path)
	return err == nil && readable
}
