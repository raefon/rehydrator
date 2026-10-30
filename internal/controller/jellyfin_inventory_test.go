package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"github.com/raefon/rehydrator/internal/model"
)

func TestInventoryReconciliationMoviesEpisodesWithoutReadProbes(t *testing.T) {
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
	tenant := fmt.Sprintf("inventory-controller-%d", time.Now().UnixNano())
	// The database is disposable; no production connections are used.
	for i := 1; i <= 2; i++ {
		if _, err = repo.UpsertImportedMovie(ctx, tenant, i, "Movie", fmt.Sprintf("/storage/movies/%d.mkv", i), "radarr", time.Now(), 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	episode, err := repo.UpsertImportedEpisode(ctx, tenant, 1, 2, 3, "Show", "Episode", 1, 1, "/storage/tv/1.mkv", "sonarr", time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveTorrentMetadata(ctx, episode.ID, model.TorrentMetadata{
		Kind: model.SourceNZBFile, Protocol: model.ProtocolUsenet, DownloadID: "inventory-nzb",
		NZBFilename: "episode.nzb", NZBFile: []byte(`<nzb><file><segments><segment bytes="1" number="1">article@example</segment></segments></file></nzb>`),
	}, "sonarr"); err != nil {
		t.Fatal(err)
	}
	scans := []string{}
	broken := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items":
			if broken {
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, `{"Items":[{"Id":"known","Path":"/library/movies/1.mkv"}],"StartIndex":0,"TotalRecordCount":1}`)
		case "/Library/ScanPath":
			var body struct{ Path string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			scans = append(scans, body.Path)
			fmt.Fprint(w, `{"Status":"Created","ItemId":"new"}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New(Options{Repo: repo, Tenant: tenant, Jellyfin: jellyfin.NewClient(srv.URL, "key", time.Second, "targeted_scan"),
		JellyfinConfig: config.JellyfinConfig{Enabled: true, RefreshMode: "targeted_scan", InventorySync: config.JellyfinInventoryConfig{
			Enabled: true, IntervalSeconds: 1800, MaxRefreshesPerRun: 1, CooldownMinutes: 360, PathMappings: map[string]string{"/storage": "/library"},
		}}})
	// CSI is deliberately nil: reconciliation must not read either protocol.
	broken = true
	c.reconcileJellyfinInventory(ctx)
	if len(scans) != 0 {
		t.Fatal("API failure triggered scans")
	}
	broken = false
	c.reconcileJellyfinInventory(ctx)
	if len(scans) != 1 {
		t.Fatal("per-pass bound failed", scans)
	}
	c.reconcileJellyfinInventory(ctx)
	if len(scans) != 2 || scans[0] == scans[1] {
		t.Fatal("cooldown/fairness failed", scans)
	}
	c.reconcileJellyfinInventory(ctx)
	if len(scans) != 2 {
		t.Fatal("cooldown failed", scans)
	}
	found := map[string]bool{}
	for _, p := range scans {
		found[p] = true
	}
	if !found["/library/movies/2.mkv"] || !found["/library/tv/1.mkv"] {
		t.Fatal("missing movie/episode coverage", scans)
	}
}
