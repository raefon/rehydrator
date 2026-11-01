package gateway

import (
	"context"
	"github.com/raefon/rehydrator/internal/config"
	"testing"
	"time"
)

func TestActivePollingSchedulesSubmissionAndProcessing(t *testing.T) {
	original, p, conn := testService(t)
	cfg := config.DefaultGateway()
	cfg.GatewayStorageConfig = original.cfg
	cfg.ActivePollIntervalSeconds = 45
	s := New(cfg, original.tenant, original.repo, nil, p, "sonarr", "radarr")
	j := addTestJob(t, s)
	ctx := context.Background()
	checkDelay := func(expected float64) {
		t.Helper()
		var seconds float64
		if err := conn.QueryRow(ctx, "SELECT extract(epoch FROM next_check_at-now())::float8 FROM gateway_jobs WHERE id=$1", j.ID).Scan(&seconds); err != nil {
			t.Fatal(err)
		}
		if seconds > expected+1 || seconds < expected-5 {
			t.Fatalf("due in %.1fs, expected %.1fs", seconds, expected)
		}
	}
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	checkDelay(45)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	checkDelay(45)
	// Publishing for mount verification does not use the faster active interval.
	p.ready = true
	s.SetRole(config.RoleGateway)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	checkDelay(60)
	if original.activePollInterval != 30*time.Second {
		t.Fatal("gateway default changed")
	}
	cfg.ActivePollIntervalSeconds = 0
	if got := New(cfg, "test", nil, nil, nil).activePollInterval; got != 30*time.Second {
		t.Fatal("constructor default", got)
	}
}
