package health

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSeerrTVWebhookApprovalAndScope(t *testing.T) {
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
	tenant := fmt.Sprintf("webhook-tv-%d", time.Now().UnixNano())
	item, err := repo.UpsertImportedEpisode(ctx, tenant, 20, 30, 40, "Show", "Episode", 1, 1, "/show/episode.mkv", "sonarr", time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveTorrentMetadata(ctx, item.ID, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sonarr"); err != nil {
		t.Fatal(err)
	}
	// Archived is a valid TV rearm candidate without pretending the imported ID is a series ID.
	if err = repo.MarkArchived(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	h := apiHandler{repo: repo, tenant: tenant, token: "secret", requireToken: true}
	send := func(event, scope string) {
		r := httptest.NewRequest("POST", "/api/seerr/webhook", strings.NewReader(fmt.Sprintf(`{"notification_type":%q,"requestId":"42","media":{"mediaType":"tv","tmdbId":"100","tvdbId":"200"}%s}`, event, scope)))
		r.Header.Set("X-Rehydrator-Token", "secret")
		w := httptest.NewRecorder()
		h.handleSeerrWebhook(w, r)
		if w.Code != 202 {
			t.Fatalf("status=%d %s", w.Code, w.Body)
		}
	}
	send("MEDIA_PENDING", `,"seasons":[1]`)
	got, _, _ := repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if got.RearmRequested {
		t.Fatal("pending request triggered rearm")
	}
	send("MEDIA_APPROVED", "")
	got, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if got.RearmRequested {
		t.Fatal("unscoped request widened to series")
	}
	send("MEDIA_APPROVED", `,"seasons":[1]`)
	got, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if !got.RearmRequested {
		t.Fatal("approved scoped request did not rearm")
	}
	if err = repo.MarkAvailable(ctx, item.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now()); err != nil {
		t.Fatal(err)
	}
	send("MEDIA_APPROVED", `,"seasons":[1]`)
	got, _, _ = repo.GetState(ctx, tenant, model.MediaSeries, 40)
	if got.RearmRequested {
		t.Fatal("webhook replay rearmed again")
	}
}
