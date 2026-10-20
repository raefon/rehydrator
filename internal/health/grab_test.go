package health

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

func TestGrabWebhookCapture(t *testing.T) {
	for _, arr := range []string{"radarr", "sonarr"} {
		owner := "movie"
		if arr == "sonarr" {
			owner = "series"
		}
		body := `{"eventType":"Grab","` + owner + `":{"id":77},"downloadId":"ABC123","release":{"releaseTitle":"Example"}}`
		called := false
		h := apiHandler{tenant: "test", token: "secret", requireToken: true, captureGrab: func(ctx context.Context, g model.GrabSource) error {
			called = true
			if g.Arr != arr || g.OwnerID != 77 || g.DownloadID != "ABC123" || g.Torrent.SourceTitle != "Example" {
				t.Fatalf("bad grab: %+v", g)
			}
			return nil
		}}
		r := httptest.NewRequest("POST", "/api/webhook/"+arr, strings.NewReader(body))
		r.Header.Set("X-Rehydrator-Token", "secret")
		w := httptest.NewRecorder()
		if arr == "radarr" {
			h.handleRadarrWebhook(w, r)
		} else {
			h.handleSonarrWebhook(w, r)
		}
		if w.Code != 202 || !called {
			t.Fatalf("status=%d called=%v body=%s", w.Code, called, w.Body.String())
		}
		// Unauthorized webhooks cannot start capture.
		called = false
		r = httptest.NewRequest("POST", "/", strings.NewReader(body))
		w = httptest.NewRecorder()
		if arr == "radarr" {
			h.handleRadarrWebhook(w, r)
		} else {
			h.handleSonarrWebhook(w, r)
		}
		if w.Code != 401 || called {
			t.Fatal("unauthorized capture accepted")
		}
	}
}

func TestUsenetGrabProtocol(t *testing.T) {
	g := grabFromWebhook("tenant", "sonarr", map[string]any{"series": map[string]any{"id": 55}, "downloadId": "opaque-Job-ID", "downloadClientType": "Sabnzbd", "release": map[string]any{"downloadProtocol": "Usenet", "downloadUrl": "https://indexer.example/release.nzb"}})
	if g.Torrent.Protocol != model.ProtocolUsenet || g.Torrent.InfoHash != "" || g.DownloadID != "opaque-Job-ID" || g.OwnerID != 55 {
		t.Fatalf("unexpected source: %+v", g)
	}
}
