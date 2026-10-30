package syncer

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/seerr"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSeerrTVPollPendingToApproved(t *testing.T) {
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
	tenant := fmt.Sprintf("poll-tv-%d", time.Now().UnixNano())
	item, err := repo.UpsertImportedEpisode(ctx, tenant, 20, 30, 40, "Show", "Episode", 1, 1, "/show/episode.mkv", "sonarr", time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sonarr"); err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkArchived(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	status := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[{"id":42,"status":%d,"media":{"mediaType":"tv","tmdbId":100,"tvdbId":200},"seasons":[{"seasonNumber":1}]}]}`, status)
	}))
	defer srv.Close()
	s := NewSeerr(SeerrOptions{Repo: repo, Tenant: tenant, Seerr: seerr.NewClient(srv.URL, "key")})
	if err = s.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _, _ := repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if got.RearmRequested {
		t.Fatal("pending poll rearmed")
	}
	status = 2
	if err = s.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if !got.RearmRequested {
		t.Fatal("approval transition not reconciled")
	}
	if err = repo.MarkAvailable(ctx, item.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if got.RearmRequested {
		t.Fatal("poll replay rearmed")
	}
}
