package health

import (
	"encoding/json"
	"github.com/raefon/rehydrator/internal/seerr"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaybackWrongEditionIgnoredBeforeDatabaseAccess(t *testing.T) {
	for _, roots := range [][]string{{"/storage/media/movies"}, {"/storage/media/movies-4k"}} {
		h := &apiHandler{libraryPaths: roots}
		path := "/storage/media/movies-4k/Movie/movie.mkv"
		if roots[0] == "/storage/media/movies-4k" {
			path = "/storage/media/movies/Movie/movie.mkv"
		}
		w := httptest.NewRecorder()
		h.handlePlaybackIntent(w, httptest.NewRequest("POST", "/", nil), playbackIntent{Event: "PlaybackStart", Path: path})
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestVerified4KSeerrWebhookForwarded(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/seerr/webhook" || r.Header.Get("X-Rehydrator-Token") != "shared-token" {
			t.Error("invalid forwarding")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["is4k"] != true {
			t.Error("missing verified edition")
		}
		w.WriteHeader(202)
	}))
	defer target.Close()
	lookup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":42,"is4k":true,"status":2,"media":{"mediaType":"movie","tmdbId":100}}`))
	}))
	defer lookup.Close()
	h := &apiHandler{seerr: seerr.NewClient(lookup.URL, "key"), seerr4KControllerURL: target.URL, requireToken: true, token: "shared-token"}
	r := httptest.NewRequest("POST", "/api/seerr/webhook", strings.NewReader(`{"requestId":42,"media":{"mediaType":"movie","tmdbId":100}}`))
	r.Header.Set("X-Rehydrator-Token", "shared-token")
	w := httptest.NewRecorder()
	h.handleSeerrPost(w, r, false)
	if w.Code != 202 || !called {
		t.Fatal(w.Code, w.Body.String())
	}
}
