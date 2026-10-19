package health

import (
	"context"
	"errors"
	"github.com/raefon/rehydrator/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJellyfinIntent(t *testing.T) {
	intent := playbackIntentFromJellyfin(map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie", "Provider_tmdb": "123", "Name": "Movie", "ItemId": "999"})
	if intent.Source != "jellyfin" || intent.MediaType != model.MediaMovie || intent.TMDBID != 123 || intent.ArrID != 0 {
		t.Fatalf("unexpected intent: %+v", intent)
	}
	episode := playbackIntentFromJellyfin(map[string]any{"ItemType": "Episode", "Path": "/media/episode.mkv"})
	if episode.MediaType != model.MediaSeries || episode.Path != "/media/episode.mkv" {
		t.Fatalf("unexpected episode: %+v", episode)
	}
}

func TestJellyfinWebhookGuards(t *testing.T) {
	for _, tc := range []struct {
		method, body, token string
		enabled             bool
		status              int
	}{
		{"GET", "", "secret", true, 405},
		{"POST", `{}`, "wrong", true, 401},
		{"POST", `{}`, "secret", false, 404},
		{"POST", `invalid`, "secret", true, 400},
		{"POST", `{"NotificationType":"PlaybackStop","ItemType":"Movie"}`, "secret", true, 202},
		{"POST", `{"NotificationType":"PlaybackProgress","ItemType":"Movie"}`, "secret", true, 202},
		{"POST", `{"NotificationType":"PlaybackStart","ItemType":"Episode"}`, "secret", true, 400},
		{"POST", `{"NotificationType":"PlaybackStart","ItemType":"Audio"}`, "secret", true, 202},
	} {
		h := apiHandler{token: "secret", requireToken: true, playbackEnabled: tc.enabled}
		r := httptest.NewRequest(tc.method, "/api/playback/jellyfin", strings.NewReader(tc.body))
		r.Header.Set("X-Rehydrator-Token", tc.token)
		w := httptest.NewRecorder()
		h.handleJellyfinPlayback(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.body, w.Code, tc.status)
		}
	}
}

func TestJellyfinPlaybackPassesUserContext(t *testing.T) {
	called := false
	h := apiHandler{playbackEnabled: true, jellyfinItemPath: func(ctx context.Context, itemID, userID string) (string, error) {
		called = true
		if itemID != "item-123" || userID != "user-456" {
			t.Fatalf("lookup context: item=%s user=%s", itemID, userID)
		}
		return "", errors.New("upstream failed")
	}}
	r := httptest.NewRequest("POST", "/api/playback/jellyfin", strings.NewReader(`{"NotificationType":"PlaybackStart","ItemType":"Episode","ItemId":"item-123","UserId":"user-456"}`))
	w := httptest.NewRecorder()
	h.handleJellyfinPlayback(w, r)
	if !called || w.Code != 502 {
		t.Fatalf("called=%v status=%d", called, w.Code)
	}
}
