package controller

import (
	"context"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileHealthOutageGuard(t *testing.T) {
	for _, tc := range []struct {
		name         string
		reads        []bool
		canary, want bool
	}{
		{"one missing no canary", []bool{false}, false, true},
		{"one missing with canary", []bool{false}, true, false},
		{"isolated failure", []bool{false, true, true}, false, false},
		{"widespread failure", []bool{false, false, true}, false, true},
		{"all failures", []bool{false, false, false}, true, true},
		{"healthy", []bool{true, true, true}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var results []db.FileHealthResult
			for _, read := range tc.reads {
				results = append(results, db.FileHealthResult{Readable: read})
			}
			if got := fileHealthOutage(results, tc.canary, 50); got != tc.want {
				t.Fatalf("outage=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestRearmRequiresReadableBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{CSI: csi.NewChecker(""), CSIVisibilityTimeout: 10 * time.Millisecond, CSIVisibilityPoll: time.Millisecond})
	if c.waitForCSI(context.Background(), path) {
		t.Fatal("empty file metadata completed rearm")
	}
	if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	if !c.waitForCSI(context.Background(), path) {
		t.Fatal("readable file not accepted")
	}
}

func TestHealthSampleContinuesAfterTimeout(t *testing.T) {
	targets := []db.FileHealthTarget{{Path: "slow"}, {Path: "healthy"}, {Path: "busy"}, {Path: "same-path-busy"}}
	results, timeouts := sampleFileHealth(context.Background(), targets, func(path string) (bool, error) {
		switch path {
		case "slow":
			return false, context.DeadlineExceeded
		case "busy":
			return false, csi.ErrProbeBusy
		case "same-path-busy":
			return false, csi.ErrProbePathBusy
		default:
			return true, nil
		}
	})
	if len(results) != 4 || timeouts != 1 || !results[1].Readable || !results[2].Deferred || !results[3].Deferred {
		t.Fatalf("results=%+v timeouts=%d", results, timeouts)
	}
	if fileHealthOutage(results, false, 50) {
		t.Fatal("deferred reads treated as mount failures")
	}
}

func TestProviderOutageCountsReleasesNotEpisodes(t *testing.T) {
	states := map[string]db.ProviderHealth{"pack": {Status: "unavailable"}}
	if providerSampleOutage(states, 50, 3) {
		t.Fatal("one pack treated as a mass outage")
	}
	states["other"] = db.ProviderHealth{Status: "unavailable"}
	states["healthy"] = db.ProviderHealth{Status: "available"}
	if !providerSampleOutage(states, 50, 3) {
		t.Fatal("mass outage not suppressed")
	}
}

type readyHealthClient struct {
	calls  int
	status string
}

func (h *readyHealthClient) Availability(ctx context.Context, hashes []string) (map[string]torbox.Observation, error) {
	h.calls++
	return map[string]torbox.Observation{hashes[0]: {Status: torbox.Availability(h.status)}}, nil
}
func TestProviderReadinessSharedCache(t *testing.T) {
	health := &readyHealthClient{status: "unavailable"}
	c := New(Options{TorBoxHealth: config.TorBoxHealthConfig{Enabled: true, TimeoutSeconds: 1}, TorBoxHealthClient: health})
	for i := 0; i < 3; i++ {
		if ready, err := c.torboxReady(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil || ready {
			t.Fatal("missing provider data accepted", err)
		}
	}
	if health.calls != 1 {
		t.Fatal("same release was queried per episode", health.calls)
	}
	health.status = "available"
	c.providerReadyCache = nil
	if ready, err := c.torboxReady(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil || !ready {
		t.Fatal("provider availability ignored", err)
	}
}

func TestConfirmedProviderLossDoesNotPauseRecovery(t *testing.T) {
	states := map[string]db.ProviderHealth{
		"a": {Status: "unavailable", Failures: 3},
		"b": {Status: "unavailable", Failures: 3},
		"c": {Status: "unavailable", Failures: 3},
	}
	if providerSampleOutage(states, 50, 3) {
		t.Fatal("confirmed losses treated as outage")
	}
	for hash := range states {
		states[hash] = db.ProviderHealth{Status: "unavailable", Failures: 2}
	}
	if !providerSampleOutage(states, 50, 3) {
		t.Fatal("unconfirmed widespread loss not paused")
	}
	states["a"] = db.ProviderHealth{Status: "unknown"}
	states["b"] = db.ProviderHealth{Status: "unknown"}
	states["c"] = db.ProviderHealth{Status: "unknown"}
	if providerSampleOutage(states, 50, 3) {
		t.Fatal("unknown results treated as confirmed absence")
	}
}
