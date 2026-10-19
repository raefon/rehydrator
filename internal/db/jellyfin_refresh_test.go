package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestJellyfinRefreshAuditAndSelfHeal(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := "jellyfin-test-" + time.Now().Format("150405.000000000")
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_jellyfin_refreshes WHERE tenant=$1`, tenant)
	item, err := repo.UpsertImportedMovie(ctx, tenant, 7, "Movie", "/media/movie.mkv", "radarr", time.Now().Add(time.Hour), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAvailable(ctx, item.ID, "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	candidates, err := repo.JellyfinSelfHealWorkItems(ctx, tenant, 5, 86400)
	if err != nil || len(candidates) != 1 {
		t.Fatal("restored item missing from self-heal", err)
	}
	if err := repo.RecordJellyfinRefresh(ctx, tenant, item.ID, 7, "test", "movie_file", item.SymlinkPath, "failed", "failure"); err != nil {
		t.Fatal(err)
	}
	candidates, err = repo.JellyfinSelfHealWorkItems(ctx, tenant, 5, 86400)
	if err != nil || len(candidates) != 1 {
		t.Fatal("failed refresh suppressed self-heal", err)
	}
	if err := repo.RecordJellyfinRefresh(ctx, tenant, item.ID, 7, "test", "movie_file", item.SymlinkPath, "success", ""); err != nil {
		t.Fatal(err)
	}
	candidates, err = repo.JellyfinSelfHealWorkItems(ctx, tenant, 5, 86400)
	if err != nil || len(candidates) != 0 {
		t.Fatal("successful Jellyfin refresh not recognized", err)
	}
	if recent, err := repo.RecentSuccessfulJellyfinRefresh(ctx, tenant, item.SymlinkPath, time.Now().Add(-time.Hour)); err != nil || !recent {
		t.Fatal("database dedupe failed", err)
	}
	if err := repo.RecordJellyfinRefresh(ctx, tenant, "", 0, "manual_library_refresh", "library", "", "success", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordJellyfinRefresh(ctx, tenant, item.ID, 7, "test", "movie_file", item.SymlinkPath, "skipped", "deduped"); err != nil {
		t.Fatal(err)
	}
	metrics, err := repo.Metrics(ctx, tenant)
	if err != nil || metrics.JellyfinRefreshFailures != 1 {
		t.Fatal("intentional dedupe counted as failure", err)
	}

	if audits, err := repo.RecentJellyfinRefreshes(ctx, tenant, 20); err != nil || len(audits) != 4 {
		t.Fatal("audit listing failed", err)
	}
}
