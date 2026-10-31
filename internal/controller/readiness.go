package controller

import (
	"context"
	"time"

	"fmt"
	"github.com/raefon/rehydrator/internal/cloudusenet"
	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) rearmReady(ctx context.Context, item model.MediaCacheState, hash string) (bool, error) {
	if c.directGateway() {
		id := hash
		if item.SourceProtocol == model.ProtocolUsenet {
			id = item.SourceDownloadID
		}
		ready, err := c.opt.CloudUsenet.Ready(ctx, id)
		return ready && c.pathReadable(ctx, item.SymlinkPath), err
	}
	if item.SourceProtocol == model.ProtocolUsenet {
		job := item.DownloadJobID
		if job == "" {
			job = hash
		}
		if cloudusenet.IsJob(job) || cloudusenet.IsJob(item.SourceDownloadID) {
			if c.opt.CloudUsenet == nil {
				return false, fmt.Errorf("cloud Usenet backend is disabled")
			}
			if !cloudusenet.IsJob(job) {
				job = item.SourceDownloadID
			}
			ready, err := c.opt.CloudUsenet.Ready(ctx, job)
			if err != nil || !ready {
				return false, err
			}
			return c.pathReadable(ctx, item.SymlinkPath), nil
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

func isCloudItem(item model.MediaCacheState) bool {
	return cloudusenet.IsJob(item.SourceDownloadID) || cloudusenet.IsJob(item.DownloadJobID)
}

func (c *Controller) directGateway() bool {
	return c.opt.CloudUsenet != nil && c.opt.CloudUsenet.Direct()
}
