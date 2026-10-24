package db

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
	"os"
	"testing"
	"time"
)

func TestProviderHealthConsistencyAndSharedRecovery(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("provider-health-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_provider_health WHERE tenant=$1`, tenant)
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_file_health_runs WHERE tenant=$1`, tenant)
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i := 0; i < 3; i++ {
		m, err := r.UpsertImportedEpisode(ctx, tenant, 55, i+1, 100+i, "Show", "Episode", 1, i+1, fmt.Sprintf("/show/%d.mkv", i), "sonarr", time.Now().Add(time.Hour), 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.SaveTorrentMetadata(ctx, m.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + hash}, "sonarr"); err != nil {
			t.Fatal(err)
		}
	}
	run := func(status torbox.Availability, filesystemOutage bool) int {
		t.Helper()
		p, err := r.AcquireFileHealthPass(ctx, tenant, 0)
		if err != nil || p == nil {
			t.Fatal(err)
		}
		defer p.Close(ctx)
		h, err := p.RecordProviderHealth(ctx, hash, torbox.Observation{Status: status, Reason: "test"}, 3)
		if err != nil {
			t.Fatal(err)
		}
		cached, fresh, err := p.ProviderHealth(ctx, hash, time.Hour)
		if err != nil || !fresh || cached.Failures != h.Failures {
			t.Fatal("cached confirmation lost", err)
		}
		targets, err := p.Targets(ctx, 20)
		if err != nil {
			t.Fatal(err)
		}
		var results []FileHealthResult
		for _, target := range targets {
			results = append(results, FileHealthResult{Target: target, Provider: &h})
		}
		n, err := p.Finish(ctx, results, filesystemOutage, 3, 2)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Mount read failures must not re-add a release the provider says is present.
	for i := 0; i < 4; i++ {
		if run(torbox.Available, false) != 0 {
			t.Fatal("mount-only failure caused provider re-add")
		}
	}
	if run(torbox.Unavailable, true) != 0 {
		t.Fatal("one missing snapshot triggered rearm")
	}
	if run(torbox.Unknown, true) != 0 {
		t.Fatal("unknown became eviction")
	}
	// Unknown resets consecutive provider failure evidence.
	if run(torbox.Unavailable, true) != 0 || run(torbox.Unavailable, true) != 0 {
		t.Fatal("confirmation threshold ignored")
	}
	if n := run(torbox.Unavailable, true); n != 1 {
		t.Fatalf("wanted one shared release submission, got %d", n)
	}
	var broken, waiting int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='BROKEN'),count(*) FILTER(WHERE state='WAITING_FOR_VISIBILITY') FROM media_cache_state WHERE tenant=$1`, tenant).Scan(&broken, &waiting); err != nil || broken != 1 || waiting != 2 {
		t.Fatalf("broken=%d waiting=%d err=%v", broken, waiting, err)
	}
	// Catalog reconciliation never deletes source records or media rows.
	var rows int
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant=$1`, tenant).Scan(&rows)
	if rows != 3 {
		t.Fatal("inventory comparison deleted media")
	}
	report, err := r.ProviderHealthReport(ctx, tenant, 200)
	if err != nil || len(report) != 1 || report[0].TrackedFiles != 3 || report[0].InfoHash != hash {
		t.Fatalf("report=%+v err=%v", report, err)
	}

}
