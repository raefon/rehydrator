package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Controller) CaptureGrab(ctx context.Context, g model.GrabSource) error {
	g.Tenant = c.opt.Tenant
	if g.OwnerID <= 0 || strings.TrimSpace(g.DownloadID) == "" || (g.Arr != "radarr" && g.Arr != "sonarr") {
		return fmt.Errorf("grab requires Arr owner and download ID")
	}
	queued, err := c.opt.Repo.QueueGrabSource(ctx, g)
	if err != nil {
		return err
	}
	if !queued {
		return c.opt.Repo.LinkGrabSources(ctx, c.opt.Tenant)
	}
	// Persist before network I/O so a webhook timeout doesn't lose the work.
	captureCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := c.captureGrabSource(captureCtx, g); err != nil {
		slog.Warn("grab source capture pending; worker will retry", "arr", g.Arr, "owner_id", g.OwnerID, "error", err)
		_ = c.opt.Repo.RetryGrabSource(ctx, g, err.Error())
	}
	return c.opt.Repo.LinkGrabSources(ctx, c.opt.Tenant)
}

func (c *Controller) RecordGrabImport(ctx context.Context, g model.GrabSource, path string) error {
	g.Tenant = c.opt.Tenant
	if g.OwnerID <= 0 || g.DownloadID == "" {
		return nil
	}
	if err := c.opt.Repo.RecordGrabImport(ctx, g, path); err != nil {
		return err
	}
	return c.opt.Repo.LinkGrabSources(ctx, c.opt.Tenant)
}

func (c *Controller) captureGrabSource(ctx context.Context, g model.GrabSource) error {
	if c.opt.Gateway == nil {
		return model.ErrSourceRecoveryRequired
	}
	source, err := c.opt.Gateway.Source(ctx, g.DownloadID)
	if err != nil {
		return err
	}
	g.Torrent = source
	return c.opt.Repo.CompleteGrabSource(ctx, g)
}

func (c *Controller) reconcileGrabSources(ctx context.Context) {
	pending, err := c.opt.Repo.PendingGrabSources(ctx, c.opt.Tenant)
	if err != nil {
		slog.Warn("grab source queue lookup failed", "error", err)
		return
	}
	for _, g := range pending {
		captureCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := c.captureGrabSource(captureCtx, g)
		cancel()
		if err != nil {
			_ = c.opt.Repo.RetryGrabSource(ctx, g, err.Error())
			slog.Warn("grab source capture retry failed", "arr", g.Arr, "owner_id", g.OwnerID, "error", err)
		}
	}
	if err := c.opt.Repo.LinkGrabSources(ctx, c.opt.Tenant); err != nil {
		slog.Warn("grab source import linking failed", "error", err)
	}
}
