package health

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/db"
)

func TestJellyfinRoutesReplaceRetiredIntegration(t *testing.T) {
	movies, episodes, libraries := 0, 0, 0
	server := NewAPIServer(APIOptions{Repo: &db.Repo{}, Token: "secret", RequireToken: true, PlaybackEnabled: true,
		JellyfinRefreshMovie:   func(ctx context.Context, id int) error { movies = id; return nil },
		JellyfinRefreshSeries:  func(ctx context.Context, id int) error { episodes = id; return nil },
		JellyfinRefreshLibrary: func(ctx context.Context) error { libraries++; return nil },
	})
	for _, path := range []string{"/api/plex/refresh/movie/7", "/api/plex/refresh/series/8", "/api/plex/refresh/movies", "/api/plex/refresh/tv", "/api/plex/refreshes/recent", "/api/playback/plex"} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("X-Rehydrator-Token", "secret")
		w := httptest.NewRecorder()
		server.srv.Handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Errorf("retired route %s returned %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/jellyfin/refresh/movie/7", "/api/jellyfin/refresh/series/8", "/api/jellyfin/refresh/library"} {
		r := httptest.NewRequest("POST", path, nil)
		w := httptest.NewRecorder()
		server.srv.Handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("route %s missing auth", path)
		}
		r = httptest.NewRequest("POST", path, nil)
		r.Header.Set("X-Rehydrator-Token", "secret")
		w = httptest.NewRecorder()
		server.srv.Handler.ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	if movies != 7 || episodes != 8 || libraries != 1 {
		t.Fatal("callbacks not wired correctly")
	}
	r := httptest.NewRequest("POST", "/api/playback/jellyfin", strings.NewReader(`{"NotificationType":"PlaybackStop"}`))
	r.Header.Set("X-Rehydrator-Token", "secret")
	w := httptest.NewRecorder()
	server.srv.Handler.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal("Jellyfin playback route unavailable")
	}
}
