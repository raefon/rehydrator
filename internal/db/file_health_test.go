package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

func TestFileHealthRecoveryPolicy(t *testing.T) {
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
	tenant := fmt.Sprintf("health-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_file_health_runs WHERE tenant=$1`, tenant)
	var ids []string
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i := 0; i < 4; i++ {
		m, err := r.UpsertImportedMovie(ctx, tenant, 100+i, "Movie", fmt.Sprintf("/media/%d.mkv", i), "radarr", time.Now().Add(time.Hour), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
		if err := r.SaveTorrentMetadata(ctx, m.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + hash}, "radarr"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.pool.Exec(ctx, `UPDATE media_cache_state SET state='ARCHIVED' WHERE id=$1`, ids[3]); err != nil {
		t.Fatal(err)
	}
	pass, err := r.AcquireFileHealthPass(ctx, tenant, 0)
	if err != nil || pass == nil {
		t.Fatal("pass not acquired", err)
	}
	if other, err := r.AcquireFileHealthPass(ctx, tenant, 0); err != nil || other != nil {
		t.Fatal("concurrent tenant sweep allowed", err)
	}
	pass.Close(ctx)
	run := func(readable map[string]bool, outage bool, max int) int {
		t.Helper()
		p, err := r.AcquireFileHealthPass(ctx, tenant, 0)
		if err != nil || p == nil {
			t.Fatal("pass", err)
		}
		defer p.Close(ctx)
		targets, err := p.Targets(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		results := make([]FileHealthResult, 0, len(targets))
		for _, target := range targets {
			if target.ID == ids[3] {
				t.Fatal("archived item selected")
			}
			results = append(results, FileHealthResult{Target: target, Readable: readable[target.ID]})
		}
		queued, err := p.Finish(ctx, results, outage, 3, max)
		if err != nil {
			t.Fatal(err)
		}
		return queued
	}
	if run(nil, false, 2) != 0 || run(nil, false, 2) != 0 {
		t.Fatal("queued before three failures")
	}
	// A successful read resets confirmation for that file.
	if run(map[string]bool{ids[0]: true}, false, 1) != 1 {
		t.Fatal("per-pass cap or confirmation broken")
	}
	var failures int
	if err := r.pool.QueryRow(ctx, `SELECT consecutive_failures FROM media_cache_file_health WHERE media_id=$1`, ids[0]).Scan(&failures); err != nil || failures != 0 {
		t.Fatal("healthy count not reset", err)
	}
	// Outages don't queue anything, and discard the sample's uncertain failures.
	if run(nil, true, 2) != 0 {
		t.Fatal("outage queued recovery")
	}
	if err := r.pool.QueryRow(ctx, `SELECT consecutive_failures FROM media_cache_file_health WHERE media_id=$1`, ids[2]).Scan(&failures); err != nil || failures != 0 {
		t.Fatal("outage count not reset", err)
	}
	// A committed pass enforces interval even if the process is restarted.
	if p, err := r.AcquireFileHealthPass(ctx, tenant, time.Hour); err != nil || p != nil {
		t.Fatal("durable interval ignored", err)
	}
	// Automatic recovery has a durable 24-hour per-file/source cooldown.
	var recoveredID string
	if err := r.pool.QueryRow(ctx, `SELECT id::text FROM media_cache_state WHERE tenant=$1 AND state='BROKEN' LIMIT 1`, tenant).Scan(&recoveredID); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkAvailable(ctx, recoveredID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	cooldownPass, err := r.AcquireFileHealthPass(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	cooldownTargets, err := cooldownPass.Targets(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range cooldownTargets {
		if target.ID == recoveredID {
			target.Failures = 3
			if n, err := cooldownPass.Finish(ctx, []FileHealthResult{{Target: target}}, false, 3, 2); err != nil || n != 0 {
				t.Fatal("automatic recovery cooldown ignored", err)
			}
		}
	}
	cooldownPass.Close(ctx)
	// Busy paths are observations of scheduling, not evidence of failed reads.
	if _, err := r.pool.Exec(ctx, `UPDATE media_cache_file_health SET consecutive_failures=2 WHERE media_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	deferredPass, err := r.AcquireFileHealthPass(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	deferredTargets, err := deferredPass.Targets(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range deferredTargets {
		if target.ID == ids[0] {
			if n, err := deferredPass.Finish(ctx, []FileHealthResult{{Target: target, Deferred: true}}, false, 3, 2); err != nil || n != 0 {
				t.Fatal("deferred path queued", err)
			}
		}
	}
	deferredPass.Close(ctx)
	if err := r.pool.QueryRow(ctx, `SELECT consecutive_failures FROM media_cache_file_health WHERE media_id=$1`, ids[0]).Scan(&failures); err != nil || failures != 2 {
		t.Fatal("deferred observation changed failure evidence", failures, err)
	}
	// A path/source change must discard previous failures.
	if _, err := r.pool.Exec(ctx, `UPDATE media_cache_file_health SET consecutive_failures=9 WHERE media_id=$1;`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE media_cache_state SET symlink_path='/media/new-release.mkv' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	p, err := r.AcquireFileHealthPass(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)
	targets, err := p.Targets(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ID == ids[0] && target.Failures != 0 {
			t.Fatal("upgrade inherited old failures")
		}
	}
	// A concurrent archive/rearm transition wins over the health observation.
	if _, err := r.pool.Exec(ctx, `UPDATE media_cache_state SET state='ARCHIVED' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	queued, err := p.Finish(ctx, []FileHealthResult{{Target: FileHealthTarget{ID: ids[0], Path: "/media/new-release.mkv", SourceKey: "torrent:" + hash, Failures: 8}}}, false, 3, 2)
	if err != nil || queued != 0 {
		t.Fatal("archived item rearmed from stale sample", err)
	}
}
