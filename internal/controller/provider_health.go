package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
)

func (c *Controller) observeProviders(ctx context.Context, pass *db.FileHealthPass, targets []db.FileHealthTarget) (map[string]db.ProviderHealth, error) {
	states := map[string]db.ProviderHealth{}
	var due []string
	for _, t := range targets {
		if t.Protocol != model.ProtocolTorrent {
			continue
		}
		if _, seen := states[t.InfoHash]; seen {
			continue
		}
		health, fresh, err := pass.ProviderHealth(ctx, t.InfoHash, time.Duration(c.opt.TorBoxHealth.IntervalSeconds)*time.Second)
		if err != nil {
			return nil, err
		}
		states[t.InfoHash] = health
		if !fresh {
			due = append(due, t.InfoHash)
		}
	}
	if len(due) == 0 {
		return states, nil
	}
	var observed map[string]torbox.Observation
	var requestErr error
	cooling, _, _, err := c.opt.Repo.ProviderCooldownActive(ctx, c.opt.Tenant, "torbox_health")
	if err != nil {
		return nil, err
	}
	if cooling {
		requestErr = errTorBoxCooling{}
	} else {
		queryCtx, cancel := context.WithTimeout(ctx, time.Duration(c.opt.TorBoxHealth.TimeoutSeconds)*time.Second)
		observed, requestErr = c.opt.TorBoxHealthClient.Availability(queryCtx, due)
		cancel()
	}
	if requestErr != nil {
		slog.Warn("TorBox health lookup inconclusive; no eviction inferred", "error", requestErr)
		if isRateLimited(requestErr) {
			_ = c.opt.Repo.SetProviderCooldown(ctx, c.opt.Tenant, "torbox_health", c.opt.ProviderCooldown, "TorBox health rate limit")
		}
	}
	for _, hash := range due {
		result, ok := observed[hash]
		if requestErr != nil || !ok {
			result = torbox.Observation{Status: torbox.Unknown, Reason: "request_failed_or_incomplete"}
		}
		health, err := pass.RecordProviderHealth(ctx, hash, result, c.opt.FileHealth.FailureThreshold)
		if err != nil {
			return nil, err
		}
		states[hash] = health
	}
	return states, nil
}

type errTorBoxCooling struct{}

func (errTorBoxCooling) Error() string { return "TorBox health cooldown active" }

func providerSampleOutage(states map[string]db.ProviderHealth, percent, threshold int) bool {
	known, missing := 0, 0
	for _, h := range states {
		if h.Status == torbox.Available || h.Status == torbox.Unavailable {
			known++
			if h.Status == torbox.Unavailable && h.Failures < threshold {
				missing++
			}
		}
	}
	return known >= 3 && missing*100 >= known*percent
}

type providerReadyObservation struct {
	status torbox.Availability
	until  time.Time
}

func (c *Controller) torboxReady(ctx context.Context, hash string) (bool, error) {
	c.providerReadyMu.Lock()
	defer c.providerReadyMu.Unlock()
	if h, ok := c.providerReadyCache[hash]; ok && time.Now().Before(h.until) {
		return h.status == torbox.Available, nil
	}
	if c.opt.Repo != nil {
		cooling, _, _, err := c.opt.Repo.ProviderCooldownActive(ctx, c.opt.Tenant, "torbox_health")
		if err != nil {
			return false, err
		}
		if cooling {
			return false, errTorBoxCooling{}
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(c.opt.TorBoxHealth.TimeoutSeconds)*time.Second)
	defer cancel()
	observed, err := c.opt.TorBoxHealthClient.Availability(queryCtx, []string{hash})
	if err != nil {
		if c.opt.Repo != nil && isRateLimited(err) {
			_ = c.opt.Repo.SetProviderCooldown(ctx, c.opt.Tenant, "torbox_health", c.opt.ProviderCooldown, "TorBox health rate limit")
		}
		return false, err
	}
	h, ok := observed[hash]
	if !ok || h.Status == torbox.Unknown {
		return false, fmt.Errorf("TorBox readiness unknown")
	}
	if c.providerReadyCache == nil {
		c.providerReadyCache = map[string]providerReadyObservation{}
	}
	for k, v := range c.providerReadyCache {
		if time.Now().After(v.until) {
			delete(c.providerReadyCache, k)
		}
	}
	c.providerReadyCache[hash] = providerReadyObservation{status: h.Status, until: time.Now().Add(30 * time.Second)}
	return h.Status == torbox.Available, nil
}
