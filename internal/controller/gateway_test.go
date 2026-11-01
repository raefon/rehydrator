package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"

	"github.com/raefon/rehydrator/internal/gateway"
	"github.com/raefon/rehydrator/internal/model"
)

func TestGatewayTorrentRecovery(t *testing.T) {
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
	tenant := fmt.Sprintf("direct-controller-%d", time.Now().UnixNano())
	magnet := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&tr=udp%3A%2F%2Ftracker.example%3A80"
	j, err := repo.AddGatewaySource(ctx, tenant, "sonarr", model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: magnet})
	if err != nil {
		t.Fatal(err)
	}
	service := gateway.New(config.DefaultGateway(), tenant, repo, nil, nil, "sonarr", "radarr")
	c := New(Options{Repo: repo, Tenant: tenant, Gateway: service, CSI: csi.NewChecker("")})
	grab := model.GrabSource{Arr: "sonarr", OwnerID: 42, DownloadID: j.InfoHash, Torrent: model.TorrentMetadata{Kind: model.SourceUnresolved, TorrentURL: "https://expired.invalid/torrent"}}
	if err = c.CaptureGrab(ctx, grab); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		path := filepath.Join(t.TempDir(), "episode.mkv")
		if err = os.WriteFile(path, []byte("media"), 0600); err != nil {
			t.Fatal(err)
		}
		item, err := repo.UpsertImportedEpisode(ctx, tenant, 42, i, 100+i, "Show", "Episode", 1, i, path, "sonarr", time.Now(), 200)
		if err != nil {
			t.Fatal(err)
		}
		source, err := service.Source(ctx, j.InfoHash)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.SaveTorrentMetadata(ctx, item.ID, source, "sonarr"); err != nil {
			t.Fatal(err)
		}
		item, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 100+i)
		if source, err = c.resolveRearmTorrent(ctx, item); err != nil || source.Magnet != magnet {
			t.Fatal(source, err)
		}
		if ready, err := c.rearmReady(ctx, item, j.InfoHash); err != nil || ready {
			t.Fatal("premature readiness", ready, err)
		}
	}
	if err = repo.GatewayRecoveryWaiting(ctx, tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	j.State = "Completed"
	j.ProviderID = 123
	if err = repo.UpdateGatewayJob(ctx, tenant, j, time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		item, found, err := repo.GetState(ctx, tenant, model.MediaSeries, 100+i)
		if err != nil || !found || !item.RearmRequested || item.State != model.State("WAITING_FOR_VISIBILITY") {
			t.Fatal(item, err)
		}
		if ready, err := c.rearmReady(ctx, item, j.InfoHash); err != nil || !ready {
			t.Fatal(ready, err)
		}
		if valueOrEmpty(item.DownloadClient) != "torbox_gateway" {
			t.Fatal(item.DownloadClient)
		}
	}
	// Movie imports use exactly the same direct status/source path.
	path := filepath.Join(t.TempDir(), "movie.mkv")
	_ = os.WriteFile(path, []byte("media"), 0600)
	movie, err := repo.UpsertImportedMovie(ctx, tenant, 77, "Movie", path, "radarr", time.Now(), 123, 0)
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.Source(ctx, j.InfoHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveTorrentMetadata(ctx, movie.ID, source, "radarr"); err != nil {
		t.Fatal(err)
	}
	movie, _, _ = repo.GetState(ctx, tenant, model.MediaMovie, 77)
	if ready, err := c.rearmReady(ctx, movie, j.InfoHash); err != nil || !ready {
		t.Fatal(ready, err)
	}
	if _, err = c.resolveRearmTorrent(ctx, movie); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayNZBSourceAndReadiness(t *testing.T) {
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
	tenant := fmt.Sprintf("gateway-nzb-controller-%d", time.Now().UnixNano())
	nzb := []byte(`<nzb><file><segments><segment bytes="1" number="1">article@example</segment></segments></file></nzb>`)
	job, err := repo.AddGatewayNZB(ctx, tenant, "radarr", "original.nzb", nzb)
	if err != nil {
		t.Fatal(err)
	}
	service := gateway.New(config.DefaultGateway(), tenant, repo, nil, nil, "sonarr", "radarr")
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err = os.WriteFile(path, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	item, err := repo.UpsertImportedMovie(ctx, tenant, 77, "Movie", path, "radarr", time.Now(), 123, 0)
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.Source(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveTorrentMetadata(ctx, item.ID, source, "radarr"); err != nil {
		t.Fatal(err)
	}
	item, _, err = repo.GetState(ctx, tenant, model.MediaMovie, 77)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{Gateway: service, Repo: repo, Tenant: tenant, CSI: csi.NewChecker("")})
	original, err := c.resolveRearmTorrent(ctx, item)
	if err != nil || string(original.NZBFile) != string(nzb) || original.InfoHash != "" {
		t.Fatal(original, err)
	}
	if ready, err := c.rearmReady(ctx, item, ""); err != nil || ready {
		t.Fatal("unverified job ready", ready, err)
	}
	job.State = "Completed"
	job.ProviderID = 123
	if err = repo.UpdateGatewayJob(ctx, tenant, job, time.Minute); err != nil {
		t.Fatal(err)
	}
	if ready, err := c.rearmReady(ctx, item, ""); err != nil || !ready {
		t.Fatal("completed NZB not readable", ready, err)
	}
}
