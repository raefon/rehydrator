package controller

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
)

func TestArchivedRecoveryOptInAndSourceGuards(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("archived-controller-%d", time.Now().UnixNano())
	movie, err := repo.UpsertImportedMovie(ctx, tenant, 1, "Movie", "/media/movie.mkv", "radarr", time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	episode, err := repo.UpsertImportedEpisode(ctx, tenant, 10, 20, 30, "Show", "Episode", 1, 1, "/media/episode.mkv", "sonarr", time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	noSource, err := repo.UpsertImportedMovie(ctx, tenant, 2, "No source", "/media/no-source.mkv", "radarr", time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []model.MediaCacheState{movie, episode} {
		if err = repo.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "arr"); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []model.MediaCacheState{movie, episode, noSource} {
		if err = repo.MarkArchived(ctx, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	refresh := func(item model.MediaCacheState) model.MediaCacheState {
		t.Helper()
		got, found, err := repo.GetState(ctx, tenant, item.MediaType, item.ArrID)
		if err != nil || !found {
			t.Fatal(err)
		}
		return got
	}
	movie = refresh(movie)
	episode = refresh(episode)
	noSource = refresh(noSource)
	opt := Options{Repo: repo, Tenant: tenant, RearmEnabled: true, FileHealth: config.FileHealthConfig{Enabled: true, RecoverArchived: true}, TorBoxHealth: config.TorBoxHealthConfig{Enabled: true}}
	for _, gate := range []string{"opt-out", "pruning-enabled", "health-disabled", "provider-disabled", "rearm-disabled"} {
		trial := opt
		switch gate {
		case "opt-out":
			trial.FileHealth.RecoverArchived = false
		case "pruning-enabled":
			trial.PruneEnabled = true
		case "health-disabled":
			trial.FileHealth.Enabled = false
		case "provider-disabled":
			trial.TorBoxHealth.Enabled = false
		case "rearm-disabled":
			trial.RearmEnabled = false
		}
		c := New(trial)
		c.ReconcileImportedRecovery(ctx, movie)
		if refresh(movie).State != model.StateArchived {
			t.Fatalf("%s bypassed policy", gate)
		}
	}
	c := New(opt)
	changedPath := movie
	changedPath.SymlinkPath = "/media/replacement.mkv"
	c.ReconcileImportedRecovery(ctx, changedPath)
	if refresh(movie).State != model.StateArchived {
		t.Fatal("concurrent path change guard failed")
	}
	queued := movie
	queued.RearmRequested = true
	c.ReconcileImportedRecovery(ctx, queued)
	if refresh(movie).State != model.StateArchived {
		t.Fatal("in-flight snapshot changed")
	}
	for _, item := range []model.MediaCacheState{movie, episode, noSource} {
		c.ReconcileImportedRecovery(ctx, item)
	}
	for _, item := range []model.MediaCacheState{movie, episode} {
		got := refresh(item)
		if got.State != model.StateBroken || got.RearmRequested {
			t.Fatalf("not returned to bounded health flow: %+v", got)
		}
		source, found, err := repo.SavedTorrentSource(ctx, item.ID)
		if err != nil || !found || source.Kind != model.SourceMagnet {
			t.Fatal("source lost", err)
		}
		// Repeated inventory observations must not queue adds or reset the lifecycle.
		c.ReconcileImportedRecovery(ctx, got)
		if refresh(item).RearmRequested {
			t.Fatal("inventory bypassed health confirmations")
		}
	}
	if refresh(noSource).State != model.StateArchived {
		t.Fatal("source-less archive changed")
	}
}
