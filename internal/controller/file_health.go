package controller

import (
	"context"
	"errors"
	"github.com/raefon/rehydrator/internal/csi"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
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
		if r.Deferred {
			continue
		}
		if r.Readable {
			healthy++
		} else {
			failed++
		}
	}
	// No automatic recovery without at least one successful read on the mount.
	if healthy+failed == 0 {
		return true
	}
	if healthy == 0 && !canary {
		return true
	}
	return healthy+failed >= 3 && failed*100 >= (healthy+failed)*percent
}

func (c *Controller) reconcileFileHealth(ctx context.Context) {
	if !c.opt.FileHealth.Enabled || !c.opt.RearmEnabled || c.opt.Repo == nil || c.healthProbe == nil || c.opt.Decypharr == nil {
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
	providerStates := map[string]db.ProviderHealth{}
	if c.opt.TorBoxHealth.Enabled {
		if c.opt.TorBoxHealthClient == nil {
			slog.Warn("TorBox health enabled without a client")
			return
		}
		providerStates, err = c.observeProviders(ctx, pass, targets)
		if err != nil {
			slog.Warn("provider health persistence failed", "error", err)
			return
		}
	}
	probe := func(path string) (bool, error) {
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ProbeTimeoutSeconds)*time.Second)
		defer cancel()
		return c.healthProbe.Check(probeCtx, path)
	}
	canary := false
	canaryFailed := false
	if cfg.CanaryPath != "" {
		canary, err = probe(cfg.CanaryPath)
		if err != nil || !canary {
			slog.Warn("file health paused; mount canary unreadable", "error", err)
			canaryFailed = true
		}
	}
	readTargets := make([]db.FileHealthTarget, 0, len(targets))
	for _, t := range targets {
		if c.opt.TorBoxHealth.Enabled && t.Protocol == model.ProtocolTorrent && providerStates[t.InfoHash].Status != torbox.Available {
			continue
		}
		readTargets = append(readTargets, t)
	}
	var reads []db.FileHealthResult
	timedOut := 0
	if !canaryFailed {
		reads, timedOut = sampleFileHealth(ctx, readTargets, probe)
	}
	outage := canaryFailed || fileHealthOutage(reads, canary, cfg.OutageFailurePercent)
	providerOutage := providerSampleOutage(providerStates, cfg.OutageFailurePercent)
	byID := map[string]db.FileHealthResult{}
	for _, r := range reads {
		byID[r.Target.ID] = r
	}
	results := make([]db.FileHealthResult, 0, len(targets))
	for _, t := range targets {
		r, read := byID[t.ID]
		if !read {
			r = db.FileHealthResult{Target: t, Deferred: true}
		}
		if c.opt.TorBoxHealth.Enabled && t.Protocol == model.ProtocolTorrent {
			h := providerStates[t.InfoHash]
			r.Provider = &h
			r.ProviderOutage = providerOutage
			if h.Status == torbox.Unavailable {
				r.Deferred = false
			}
		}
		results = append(results, r)
	}
	failed, deferred := 0, 0
	for _, r := range reads {
		if r.Deferred {
			deferred++
		} else if !r.Readable {
			failed++
		}
	}
	available, missing, unknown := 0, 0, 0
	for _, h := range providerStates {
		switch h.Status {
		case torbox.Available:
			available++
		case torbox.Unavailable:
			missing++
		default:
			unknown++
		}
	}
	queued, err := pass.Finish(ctx, results, outage, cfg.FailureThreshold, cfg.MaxRearmsPerRun)
	if err != nil {
		slog.Warn("file health pass failed", "error", err)
		return
	}
	slog.Info("file health pass complete", "sampled", len(results), "checked", len(reads)-deferred, "provider_available", available, "provider_unavailable", missing, "provider_unknown", unknown, "provider_outage_paused", providerOutage, "deferred", deferred, "timed_out", timedOut, "unreadable", failed, "outage_paused", outage, "rearms_queued", queued)
}

// Continue after individual timeouts within a fixed outstanding-read budget.
// Deferred targets advance the sweep cursor without becoming failure evidence.
func sampleFileHealth(ctx context.Context, targets []db.FileHealthTarget, probe func(string) (bool, error)) ([]db.FileHealthResult, int) {
	results := make([]db.FileHealthResult, 0, len(targets))
	timeouts := 0
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		readable, err := probe(target.Path)
		if errors.Is(err, csi.ErrProbeBusy) || errors.Is(err, csi.ErrProbePathBusy) {
			results = append(results, db.FileHealthResult{Target: target, Deferred: true})
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			timeouts++
			slog.Warn("file health read timed out; continuing bounded sample", "path", target.Path)
		}
		results = append(results, db.FileHealthResult{Target: target, Readable: readable})
	}
	return results, timeouts
}
