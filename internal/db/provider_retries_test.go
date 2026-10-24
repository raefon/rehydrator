package db

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderRetrySharedDurableBudget(t *testing.T) {
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
	tenant := fmt.Sprintf("provider-retry-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", tenant)
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_provider_retries WHERE tenant=$1", tenant)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var items []model.MediaCacheState
	for i := 0; i < 2; i++ {
		item, err := r.UpsertImportedMovie(ctx, tenant, 100+i, "Movie", fmt.Sprintf("/media/%d.mkv", i), "radarr", time.Now(), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + hash}, "radarr"); err != nil {
			t.Fatal(err)
		}
		if _, err = r.pool.Exec(ctx, "UPDATE media_cache_state SET state='WAITING_FOR_VISIBILITY',rearm_requested=true WHERE id=$1", item.ID); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	claim := func(item model.MediaCacheState) bool {
		t.Helper()
		ok, err := r.ClaimProviderRetry(ctx, tenant, item.ID, hash, item.SymlinkPath)
		if err != nil {
			t.Error(err)
		}
		return ok
	}
	if claim(items[0]) || claim(items[1]) {
		t.Fatal("initial grace period bypassed")
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err = r.pool.Exec(ctx, "UPDATE media_cache_provider_retries SET next_attempt_at=now()-interval '1 second' WHERE tenant=$1", tenant); err != nil {
			t.Fatal(err)
		}
		var count atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if claim(items[i%2]) {
					count.Add(1)
				}
			}(i)
		}
		wg.Wait()
		if count.Load() != 1 {
			t.Fatalf("attempt %d claims=%d", attempt, count.Load())
		}
		var got int
		var delay float64
		if err = r.pool.QueryRow(ctx, "SELECT attempts,extract(epoch FROM next_attempt_at-now()) FROM media_cache_provider_retries WHERE tenant=$1", tenant).Scan(&got, &delay); err != nil {
			t.Fatal(err)
		}
		expected := float64(900 * (int64(1) << uint(attempt-1)))
		if got != attempt || delay < expected-10 || delay > expected+10 {
			t.Fatalf("attempts=%d delay=%v", got, delay)
		}
		if claim(items[0]) {
			t.Fatal("backoff bypassed")
		}
	}
	_, err = r.pool.Exec(ctx, "UPDATE media_cache_provider_retries SET next_attempt_at=now()-interval '1 day' WHERE tenant=$1", tenant)
	if err != nil {
		t.Fatal(err)
	}
	if claim(items[0]) {
		t.Fatal("retry budget exceeded")
	}
	if ok, err := r.ClaimProviderRetry(ctx, "wrong", items[0].ID, hash, items[0].SymlinkPath); err != nil || ok {
		t.Fatal("tenant guard failed", err)
	}
}
