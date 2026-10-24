package controller

import (
	"context"
	"errors"
	"github.com/raefon/rehydrator/internal/csi"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/db"
)

func (c *Controller) runFileHealth(ctx context.Context) {
	cfg := c.opt.FileHealth
	if cfg.IntervalSeconds < 30 || cfg.BatchSize < 1 || cfg.ProbeTimeoutSeconds < 1 {
		return
	}
	slog.Info("file health worker starting", "interval_seconds", cfg.IntervalSeconds, "batch_size", cfg.BatchSize, "failure_threshold", cfg.FailureThreshold, "max_rearms", cfg.MaxRearmsPerRun)
	ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	c.reconcileFileHealth(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileFileHealth(ctx)
		}
	}
}

func fileHealthOutage(results []db.FileHealthResult, canary bool, percent int) bool {
	if len(results) == 0 {
		return false
	}
	failed, healthy := 0, 0
	for _, r := range results {
		if r.Readable {
			healthy++
		} else {
			failed++
		}
	}
	// No automatic recovery without at least one successful read on the mount.
	if healthy == 0 && !canary {
		return true
	}
	return len(results) >= 3 && failed*100 >= len(results)*percent
}

func (c *Controller) reconcileFileHealth(ctx context.Context) {
	if !c.opt.FileHealth.Enabled || !c.opt.RearmEnabled || c.opt.Repo == nil || c.fileProbe == nil || c.opt.Decypharr == nil {
		return
	}
	cfg := c.opt.FileHealth
	pass, err := c.opt.Repo.AcquireFileHealthPass(ctx, c.opt.Tenant, time.Duration(cfg.IntervalSeconds)*time.Second)
	if err != nil {
		slog.Warn("file health cannot start", "error", err)
		return
	}
	if pass == nil {
		return
	}
	defer pass.Close(context.Background())
	if c.providerCoolingDown(ctx, "decypharr") {
		_, _ = pass.Finish(ctx, nil, true, cfg.FailureThreshold, 0)
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ProbeTimeoutSeconds)*time.Second)
	err = c.opt.Decypharr.Ping(pingCtx)
	cancel()
	if err != nil {
		slog.Warn("file health paused; Decypharr unavailable", "error", err)
		_, _ = pass.Finish(ctx, nil, true, cfg.FailureThreshold, 0)
		return
	}
	targets, err := pass.Targets(ctx, cfg.BatchSize)
	if err != nil {
		slog.Warn("file health lookup failed", "error", err)
		return
	}
	if len(targets) == 0 {
		_, _ = pass.Finish(ctx, nil, false, cfg.FailureThreshold, 0)
		return
	}
	probe := func(path string) (bool, error) {
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ProbeTimeoutSeconds)*time.Second)
		defer cancel()
		return c.fileProbe.Check(probeCtx, path)
	}
	canary := false
	if cfg.CanaryPath != "" {
		canary, err = probe(cfg.CanaryPath)
		if err != nil || !canary {
			slog.Warn("file health paused; mount canary unreadable", "error", err)
			_, _ = pass.Finish(ctx, nil, true, cfg.FailureThreshold, 0)
			return
		}
	}
	results := make([]db.FileHealthResult, 0, len(targets))
	blocked := false
	for _, target := range targets {
		readable, err := probe(target.Path)
		if errors.Is(err, csi.ErrProbeBusy) {
			blocked = true
			slog.Warn("file health paused; previous mount read remains blocked")
			break
		}
		results = append(results, db.FileHealthResult{Target: target, Readable: readable})
		if err != nil {
			slog.Warn("file health read timed out; stopping this sample", "path", target.Path, "error", err)
			break
		}
	}
	outage := blocked || fileHealthOutage(results, canary, cfg.OutageFailurePercent)
	failed := 0
	for _, result := range results {
		if !result.Readable {
			failed++
		}
	}
	queued, err := pass.Finish(ctx, results, outage, cfg.FailureThreshold, cfg.MaxRearmsPerRun)
	if err != nil {
		slog.Warn("file health pass failed", "error", err)
		return
	}
	slog.Info("file health pass complete", "checked", len(results), "unreadable", failed, "outage_paused", outage, "rearms_queued", queued)
}
