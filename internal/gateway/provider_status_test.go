package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/torbox"
)

func TestProviderObservationStoredWithoutChangingWorkflow(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	progress := 1.0
	present := false
	finished := true
	observation := (torbox.UsenetDownload{State: "Direct Unpack: Completed", Progress: &progress, Present: &present, Finished: &finished}).Status(current.ProviderID, true)
	if err = s.repo.RecordProviderStatus(ctx, s.tenant, current, observation); err != nil {
		t.Fatal(err)
	}
	if err = s.repo.RecordProviderCheckFailure(ctx, s.tenant, current); err != nil {
		t.Fatal(err)
	}
	stored, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || stored.ProviderStatus == nil || !stored.ProviderStatus.CheckFailed || stored.ProviderStatus.Progress == nil || *stored.ProviderStatus.Progress != 1 || stored.ProviderStatus.Ready || stored.State != "Downloading" {
		t.Fatal(stored, err)
	}
	// Another tenant cannot modify the observation.
	if err = s.repo.RecordProviderStatus(ctx, s.tenant+"-other", current, torbox.ProviderStatus{}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.repo.GatewayJobs(ctx, s.tenant)
	if err != nil || len(rows) != 1 || rows[0].ProviderStatus.State != "Direct Unpack: Completed" {
		t.Fatal(rows, err)
	}
	// Replacement IDs clear old placement telemetry, and late writes cannot restore it.
	replacement := stored
	replacement.ProviderID++
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, replacement, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = s.repo.RecordProviderStatus(ctx, s.tenant, current, observation); err != nil {
		t.Fatal(err)
	}
	stored, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || stored.ProviderStatus != nil {
		t.Fatal("stale observation survived provider replacement", err)
	}
	// A normal poll populates telemetry automatically, using the existing provider request.
	if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || stored.ProviderStatus == nil || stored.ProviderStatus.State != "downloading" || stored.ProviderStatus.CheckFailed {
		t.Fatal(stored, err)
	}
}
