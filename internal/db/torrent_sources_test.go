package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

// Run against a disposable database only; schema initialization is intentional.
func TestSavedTorrentSourceRoundTrip(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set REHYDRATOR_TEST_POSTGRES_URL to a disposable PostgreSQL database")
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
	tenant := "source-test-" + time.Now().Format("150405.000000000")
	item, err := repo.UpsertImportedMovie(ctx, tenant, 77, "Example", "/media/example.mkv", "radarr", time.Now().Add(time.Hour), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE id=$1`, item.ID)
	source := model.TorrentMetadata{Kind: model.SourceTorrentFile, InfoHash: "7952549975b3256302ada821a1295c5a1ca628dc", TorrentFile: []byte("d4:infod4:name7:exampleee"), TorrentFilename: "original.torrent"}
	if err := repo.SaveTorrentMetadata(ctx, item.ID, source, "radarr"); err != nil {
		t.Fatal(err)
	}
	saved, found, err := repo.SavedTorrentSource(ctx, item.ID)
	if err != nil || !found || string(saved.TorrentFile) != string(source.TorrentFile) || saved.InfoHash != source.InfoHash || saved.TorrentFilename != source.TorrentFilename {
		t.Fatalf("saved=%+v found=%v err=%v", saved, found, err)
	}
	// Invalid replacement metadata must not erase the preserved source.
	source.TorrentFile = nil
	if err := repo.SaveTorrentMetadata(ctx, item.ID, source, "radarr"); err == nil {
		t.Fatal("file source without bytes accepted")
	}
	saved, found, err = repo.SavedTorrentSource(ctx, item.ID)
	if err != nil || !found || len(saved.TorrentFile) == 0 {
		t.Fatal("saved metainfo was lost")
	}
	// A deliberately captured magnet replaces the old payload, never coexists.
	magnet := model.TorrentMetadata{Kind: model.SourceMagnet, InfoHash: source.InfoHash, Magnet: "magnet:?xt=urn:btih:" + source.InfoHash + "&tr=udp%3A%2F%2Ftracker.example"}
	if err := repo.SaveTorrentMetadata(ctx, item.ID, magnet, "radarr"); err != nil {
		t.Fatal(err)
	}
	saved, found, err = repo.SavedTorrentSource(ctx, item.ID)
	if err != nil || !found || saved.Kind != model.SourceMagnet || len(saved.TorrentFile) > 0 || saved.TorrentFilename != "" || saved.Magnet != magnet.Magnet {
		t.Fatalf("source replacement was not exclusive: %+v err=%v", saved, err)
	}
	if err := repo.MarkSourceRecoveryRequired(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	blocked, found, err := repo.GetState(ctx, tenant, model.MediaMovie, 77)
	if err != nil || !found || blocked.State != model.StateFailed || blocked.RearmRequested || blocked.InfoHash == nil || *blocked.InfoHash != source.InfoHash {
		t.Fatalf("recovery state lost identity or retained rearm: %+v err=%v", blocked, err)
	}

}
