package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
)

func TestBrokenTorrentProviderReconciliation(t *testing.T) {
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
	tenant := fmt.Sprintf("broken-provider-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_provider_health WHERE tenant=$1`, tenant)
	defer r.pool.Exec(ctx, `DELETE FROM media_cache_file_health_runs WHERE tenant=$1`, tenant)
	const pack = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const healthy = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const unknown = "cccccccccccccccccccccccccccccccccccccccc"
	ids := map[string]string{}
	specs := []struct {
		name, state, hash string
		requested, source bool
	}{
		{"pack1", "BROKEN", pack, false, true}, {"pack2", "BROKEN", pack, false, true},
		{"healthy", "BROKEN", healthy, false, true}, {"unknown", "BROKEN", unknown, false, true},
		{"archived", "ARCHIVED", pack, false, true}, {"pruning", "PRUNING", pack, false, true},
		{"rearming", "REARMING", pack, true, true}, {"requested", "REQUESTED", pack, false, true},
		{"failed", "FAILED", pack, false, true}, {"queued", "BROKEN", pack, true, true},
		{"no-source", "BROKEN", pack, false, false},
	}
	for i, spec := range specs {
		m, err := r.UpsertImportedMovie(ctx, tenant, 100+i, spec.name, "/media/"+spec.name+".mkv", "radarr", time.Now().Add(time.Hour), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids[spec.name] = m.ID
		if spec.source {
			if err := r.SaveTorrentMetadata(ctx, m.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + spec.hash}, "radarr"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.pool.Exec(ctx, `UPDATE media_cache_state SET state=$2,rearm_requested=$3 WHERE id=$1`, m.ID, spec.state, spec.requested); err != nil {
			t.Fatal(err)
		}
	}
	p, err := r.AcquireFileHealthPass(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := p.Targets(ctx, 100)
	if err != nil || len(targets) != 0 {
		t.Fatal("filesystem-only scan included broken rows", err)
	}
	p.Close(ctx)
	for check := 1; check <= 3; check++ {
		p, err := r.AcquireFileHealthPass(ctx, tenant, 0)
		if err != nil {
			t.Fatal(err)
		}
		targets, err := p.Targets(ctx, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		states := map[string]ProviderHealth{}
		for _, hash := range []string{pack, healthy, unknown} {
			status := torbox.Unavailable
			if hash == healthy {
				status = torbox.Available
			}
			if hash == unknown {
				status = torbox.Unknown
			}
			h, err := p.RecordProviderHealth(ctx, hash, torbox.Observation{Status: status}, 3)
			if err != nil {
				t.Fatal(err)
			}
			states[hash] = h
		}
		var results []FileHealthResult
		for _, target := range targets {
			if target.ID != ids["pack1"] && target.ID != ids["pack2"] && target.ID != ids["healthy"] && target.ID != ids["unknown"] {
				t.Fatalf("ineligible target selected: %+v", target)
			}
			h := states[target.InfoHash]
			results = append(results, FileHealthResult{Target: target, Provider: &h, Readable: h.Status == torbox.Available, Deferred: h.Status == torbox.Unknown})
		}
		queued, err := p.Finish(ctx, results, false, 3, 1)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if check == 3 {
			want = 1
		}
		if queued != want {
			t.Fatalf("check=%d queued=%d want=%d", check, queued, want)
		}
		p.Close(ctx)
	}
	for _, spec := range specs {
		var state string
		var requested bool
		if err := r.pool.QueryRow(ctx, `SELECT state,rearm_requested FROM media_cache_state WHERE id=$1`, ids[spec.name]).Scan(&state, &requested); err != nil {
			t.Fatal(err)
		}
		switch spec.name {
		case "pack1", "pack2":
			if !requested || (state != "BROKEN" && state != "WAITING_FOR_VISIBILITY") {
				t.Fatalf("pack recovery missing: %s %s %v", spec.name, state, requested)
			}
		case "healthy":
			if state != "WAITING_FOR_VISIBILITY" || !requested {
				t.Fatal("readable broken file was not routed through visibility confirmation")
			}
		default:
			if state != spec.state || requested != spec.requested {
				t.Fatalf("unrelated lifecycle changed: %s %s %v", spec.name, state, requested)
			}
		}
	}
}
