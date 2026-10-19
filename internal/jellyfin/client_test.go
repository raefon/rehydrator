package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "secret" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/jellyfin/Library/Media/Updated":
			if r.Method != http.MethodPost {
				t.Error("expected POST")
			}
			var body struct {
				Updates []struct{ Path, UpdateType string }
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Updates) != 1 || body.Updates[0].Path != "/media/movie.mkv" || body.Updates[0].UpdateType != "Modified" {
				t.Errorf("unexpected body: %+v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/jellyfin/Items/abc":
			_, _ = w.Write([]byte(`{"Path":"/media/show/episode.mkv"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	c := NewClient(server.URL+"/jellyfin/", "secret", time.Second)
	if err := c.RefreshPath(context.Background(), "/media/movie.mkv"); err != nil {
		t.Fatal(err)
	}
	if path, err := c.ItemPath(context.Background(), "abc"); err != nil || path != "/media/show/episode.mkv" {
		t.Fatalf("path=%q err=%v", path, err)
	}
	if _, err := c.ItemPath(context.Background(), "missing"); err == nil {
		t.Fatal("expected HTTP error")
	}
	if err := c.RefreshPath(context.Background(), ""); err == nil {
		t.Fatal("expected empty path rejection")
	}
}

func TestLibraryRefreshAndPing(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "secret" {
			t.Error("missing API key")
		}
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/System/Info" {
			w.WriteHeader(200)
		} else if r.URL.Path == "/Library/Refresh" {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "secret", time.Second)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.RefreshLibrary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != "GET /System/Info" || methods[1] != "POST /Library/Refresh" {
		t.Fatalf("requests=%v", methods)
	}
}
