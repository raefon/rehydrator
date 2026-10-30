package db

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"os"
	"testing"
	"time"
)

func TestSeerrTVSeasonScopeSourceValidationAndDedup(t *testing.T) {
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
	if err = r.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("seerr-tv-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", tenant)
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_seerr_requests WHERE tenant=$1", tenant)
	for i := 1; i <= 4; i++ {
		season := 1
		if i == 3 {
			season = 2
		}
		item, err := r.UpsertImportedEpisode(ctx, tenant, 50, i, 100+i, "Show", "Episode", season, i, fmt.Sprintf("/show/%d.mkv", i), "sonarr", time.Now(), 200)
		if err != nil {
			t.Fatal(err)
		}
		if i != 4 {
			if err = r.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sonarr"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = r.pool.Exec(ctx, "UPDATE media_cache_state SET state='FAILED' WHERE id=$1", item.ID); err != nil {
			t.Fatal(err)
		}
	}
	key, err := r.RecordSeerrTV(ctx, tenant, 42, 100, "Show", "2", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.RequestSeerrTV(ctx, tenant, key, 200, nil); err != nil || n != 0 {
		t.Fatal("unscoped request", n, err)
	}
	if n, err := r.RequestSeerrTV(ctx, tenant, key, 200, []int{1}); err != nil || n != 2 {
		t.Fatal("scoped rearm", n, err)
	}
	if n, err := r.RequestSeerrTV(ctx, tenant, key, 200, []int{1}); err != nil || n != 0 {
		t.Fatal("request replay", n, err)
	}
	var primary, waiting, untouched int
	for i := 1; i <= 4; i++ {
		item, _, err := r.GetState(ctx, tenant, model.MediaSeries, 100+i)
		if err != nil {
			t.Fatal(err)
		}
		if item.RearmRequested {
			if item.State == model.StateArchived {
				primary++
			} else if item.State == model.State("WAITING_FOR_VISIBILITY") {
				waiting++
			}
		}
		if !item.RearmRequested && item.State == model.StateFailed {
			untouched++
		}
	}
	if primary != 1 || waiting != 1 || untouched != 2 {
		t.Fatalf("primary=%d waiting=%d untouched=%d", primary, waiting, untouched)
	}
	// A second season request sharing the already-active pack waits rather than
	// scheduling a second primary add.
	key2, err := r.RecordSeerrTV(ctx, tenant, 43, 100, "Show", "2", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.RequestSeerrTV(ctx, tenant, key2, 200, []int{2}); err != nil || n != 1 {
		t.Fatal("second scope", n, err)
	}
	sibling, _, err := r.GetState(ctx, tenant, model.MediaSeries, 103)
	if err != nil || string(sibling.State) != "WAITING_FOR_VISIBILITY" || !sibling.RearmRequested {
		t.Fatal("active pack was duplicated", sibling, err)
	}

}
