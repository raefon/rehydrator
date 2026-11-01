package controller

import (
	"context"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) rearmReady(ctx context.Context, item model.MediaCacheState, hash string) (bool, error) {
	if c.directGateway() {
		id := hash
		if item.SourceProtocol == model.ProtocolUsenet {
			id = item.SourceDownloadID
		}
		ready, err := c.opt.Gateway.Ready(ctx, id)
		return ready && c.pathReadable(ctx, item.SymlinkPath), err
	}
	return false, model.ErrSourceRecoveryRequired
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

func (c *Controller) directGateway() bool {
	return c.opt.Gateway != nil
}
