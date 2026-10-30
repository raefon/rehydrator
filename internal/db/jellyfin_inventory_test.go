package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestJellyfinInventoryIncludesOldSourceLessMoviesAndEpisodes(t *testing.T) {
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
	tenant := fmt.Sprintf("inventory-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", tenant)
	movie, err := r.UpsertImportedMovie(ctx, tenant, 1, "Movie", "/media/movie.mkv", "radarr", time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	episode, err := r.UpsertImportedEpisode(ctx, tenant, 1, 2, 3, "Show", "Episode", 1, 1, "/media/episode.mkv", "sonarr", time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := r.UpsertImportedMovie(ctx, tenant, 4, "Archived", "/media/archived.mkv", "radarr", time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.pool.Exec(ctx, "UPDATE media_cache_state SET state='ARCHIVED' WHERE id=$1", archived.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, err := r.ImportedInventory(ctx, tenant)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	for _, item := range items {
		if item.ID != movie.ID && item.ID != episode.ID {
			t.Fatal("excluded row included")
		}
	}
	for i := 0; i < 2; i++ {
		ok, err := r.ClaimJellyfinInventory(ctx, tenant, episode.ID, episode.SymlinkPath, time.Hour)
		if err != nil || ok != (i == 0) {
			t.Fatalf("claim %d=%v %v", i, ok, err)
		}
	}
	ok, err := r.ClaimJellyfinInventory(ctx, "other", movie.ID, movie.SymlinkPath, time.Hour)
	if err != nil || ok {
		t.Fatal("cross-tenant claim", err)
	}
	ok, err = r.ClaimJellyfinInventory(ctx, tenant, archived.ID, archived.SymlinkPath, time.Hour)
	if err != nil || ok {
		t.Fatal("archived claim", err)
	}
	// Independent imported file must not be starved by a cooling missing item.
	ok, err = r.ClaimJellyfinInventory(ctx, tenant, movie.ID, movie.SymlinkPath, time.Hour)
	if err != nil || !ok {
		t.Fatal("independent file blocked", err)
	}
}
