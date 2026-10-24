package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

func TestReleaseStalePruning(t *testing.T) {
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
	tenant := fmt.Sprintf("stale-pruning-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", tenant)
	specs := []struct {
		name, state               string
		source, recent, requested bool
	}{
		{"eligible", "PRUNING", true, false, false},
		{"recent", "PRUNING", true, true, false},
		{"no-source", "PRUNING", false, false, false},
		{"archived", "ARCHIVED", true, false, false},
		{"requested", "PRUNING", true, false, true},
		{"failed", "FAILED", true, false, false},
	}
	for i, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			item, err := r.UpsertImportedMovie(ctx, tenant, 100+i, s.name, "/media/"+s.name+".mkv", "radarr", time.Now(), 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if s.source {
				if err := r.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "radarr"); err != nil {
					t.Fatal(err)
				}
			}
			age := time.Hour
			if s.recent {
				age = time.Minute
			}
			_, err = r.pool.Exec(ctx, "UPDATE media_cache_state SET state=$2,rearm_requested=$3,updated_at=now()-($4*interval '1 second') WHERE id=$1", item.ID, s.state, s.requested, age.Seconds())
			if err != nil {
				t.Fatal(err)
			}
			// First observation only starts the grace period.
			changed, err := r.ReleaseStalePruning(ctx, tenant, item.ID, item.SymlinkPath, 15*time.Minute)
			if err != nil || changed {
				t.Fatal("first observation bypassed grace", err)
			}
			if _, err := r.pool.Exec(ctx, "UPDATE media_cache_state SET pruning_recovery_seen_at=now()-($2*interval '1 second') WHERE id=$1", item.ID, age.Seconds()); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][2]string{{"other-tenant", item.SymlinkPath}, {tenant, "/wrong-path"}} {
				changed, err := r.ReleaseStalePruning(ctx, args[0], item.ID, args[1], 15*time.Minute)
				if err != nil || changed {
					t.Fatalf("scope guard failed: %v %v", changed, err)
				}
			}
			changed, err = r.ReleaseStalePruning(ctx, tenant, item.ID, item.SymlinkPath, 15*time.Minute)
			if err != nil || changed != (s.name == "eligible") {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			got, found, err := r.GetState(ctx, tenant, model.MediaMovie, 100+i)
			if err != nil || !found {
				t.Fatal(err)
			}
			want := s.state
			if changed {
				want = "BROKEN"
			}
			if string(got.State) != want || got.RearmRequested != s.requested {
				t.Fatalf("unexpected state: %+v", got)
			}
			if changed {
				again, err := r.ReleaseStalePruning(ctx, tenant, item.ID, item.SymlinkPath, 15*time.Minute)
				if err != nil || again {
					t.Fatal("reconciliation not idempotent", err)
				}
			}
		})
	}
}
