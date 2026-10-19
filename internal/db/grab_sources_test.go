package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

func TestGrabSourceImportLinking(t *testing.T) {
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
	tenant := "grab-test-" + time.Now().Format("150405.000000000")
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_grab_sources WHERE tenant=$1`, tenant)
	const hash = "7952549975b3256302ada821a1295c5a1ca628dc"
	source := model.TorrentMetadata{Kind: model.SourceTorrentFile, InfoHash: hash, TorrentFile: []byte("d4:infod4:name7:exampleee"), TorrentFilename: "original.torrent"}
	g := model.GrabSource{Tenant: tenant, Arr: "radarr", OwnerID: 77, DownloadID: hash, Torrent: source}
	if _, err := repo.QueueGrabSource(ctx, g); err != nil {
		t.Fatal(err)
	}
	pendingJobs, err := repo.PendingGrabSources(ctx, tenant)
	if err != nil || len(pendingJobs) != 1 {
		t.Fatal("grab was not persisted for retries", err)
	}
	if err := repo.RetryGrabSource(ctx, g, "history is not visible yet"); err != nil {
		t.Fatal(err)
	}
	if jobs, err := repo.PendingGrabSources(ctx, tenant); err != nil || len(jobs) != 0 {
		t.Fatal("capture backoff not applied", err)
	}
	if err := repo.CompleteGrabSource(ctx, g); err != nil {
		t.Fatal(err)
	}
	// Duplicate grab must not require another indexer fetch.
	if queued, err := repo.QueueGrabSource(ctx, g); err != nil || queued {
		t.Fatal("completed capture was queued again", err)
	}
	// Import can arrive before its media row is seeded.
	if err := repo.RecordGrabImport(ctx, g, "/media/movie.mkv"); err != nil {
		t.Fatal(err)
	}
	movie, err := repo.UpsertImportedMovie(ctx, tenant, 77, "Example", "/media/movie.mkv", "radarr", time.Now().Add(time.Hour), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	saved, found, err := repo.SavedTorrentSource(ctx, movie.ID)
	if err != nil || !found || string(saved.TorrentFile) != string(source.TorrentFile) {
		t.Fatalf("saved=%+v found=%v err=%v", saved, found, err)
	}
	if pending, err := repo.CurrentGrabSourcePending(ctx, movie.ID); err != nil || pending {
		t.Fatal("completed import still pending", err)
	}
	wrongOwner, err := repo.UpsertImportedMovie(ctx, tenant, 88, "Other", "/media/movie.mkv", "radarr", time.Now().Add(time.Hour), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSourceForItem(ctx, tenant, wrongOwner.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.SavedTorrentSource(ctx, wrongOwner.ID); err != nil || found {
		t.Fatal("source crossed media owner boundary", err)
	}
	// Recovery under the same download ID must replace an ambiguous legacy row.
	if _, err := repo.pool.Exec(ctx, `UPDATE media_cache_torrent_sources SET source_kind='recovery_required',filename='',torrent_file=NULL,magnet='' WHERE media_id=$1`, movie.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSourceForItem(ctx, tenant, movie.ID); err != nil {
		t.Fatal(err)
	}
	recovered, _, err := repo.SavedTorrentSource(ctx, movie.ID)
	if err != nil || recovered.Kind != model.SourceTorrentFile {
		t.Fatal("same-ID source recovery was not linked", err)
	}
	// A newer import must not accidentally reuse the older release's ready source.
	newer := g
	newer.DownloadID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newer.Torrent = model.TorrentMetadata{Kind: model.SourceMagnet, InfoHash: newer.DownloadID, Magnet: "magnet:?xt=urn:btih:" + newer.DownloadID}
	if err := repo.RecordGrabImport(ctx, newer, "/media/movie.mkv"); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSourceForItem(ctx, tenant, movie.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := repo.CurrentGrabSourcePending(ctx, movie.ID); err != nil || !pending {
		t.Fatal("newer pending import not protected", err)
	}
	if _, err := repo.QueueGrabSource(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteGrabSource(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	updated, _, err := repo.GetState(ctx, tenant, model.MediaMovie, 77)
	if err != nil || updated.InfoHash == nil || *updated.InfoHash != newer.DownloadID {
		t.Fatalf("new identity not linked: %+v %v", updated, err)
	}
	// The same captured season-pack source attaches to multiple imported files.
	pack := g
	pack.Arr = "sonarr"
	pack.OwnerID = 55
	if _, err := repo.QueueGrabSource(ctx, pack); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteGrabSource(ctx, pack); err != nil {
		t.Fatal(err)
	}
	for i, path := range []string{"/media/show/e1.mkv", "/media/show/e2.mkv"} {
		if err := repo.RecordGrabImport(ctx, pack, path); err != nil {
			t.Fatal(err)
		}
		episode, err := repo.UpsertImportedEpisode(ctx, tenant, 55, i+1, 100+i, "Show", "Episode", 1, i+1, path, "sonarr", time.Now().Add(time.Hour), 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.LinkGrabSourceForItem(ctx, tenant, episode.ID); err != nil {
			t.Fatal(err)
		}
		if _, found, err := repo.SavedTorrentSource(ctx, episode.ID); err != nil || !found {
			t.Fatal("pack source not attached", err)
		}
	}
}
