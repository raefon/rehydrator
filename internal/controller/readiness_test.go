package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/model"
)

func TestReadinessAfterQueueCleanup(t *testing.T) {
	const hash = "7952549975b3256302ada821a1295c5a1ca628dc"
	for _, mode := range []string{"removed_ready", "removed_missing_library", "removed_empty_file", "removed_broken_link", "removed_directory", "active", "error", "library_error", "paginated"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie.mkv")
			switch mode {
			case "removed_empty_file":
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "removed_broken_link":
				if err := os.Symlink(path+"missing", path); err != nil {
					t.Fatal(err)
				}
			case "removed_directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					if mode == "active" || mode == "error" {
						_, _ = fmt.Fprintf(w, `[{"hash":"%s","state":"%s","progress":0.5}]`, hash, mode)
						return
					}
					_, _ = fmt.Fprint(w, `[]`)
				case "/api/browse/__all__":
					if mode == "library_error" {
						w.WriteHeader(401)
						return
					}
					if mode == "removed_missing_library" {
						_, _ = fmt.Fprint(w, `{"entries":[],"total_pages":0}`)
						return
					}
					if mode == "paginated" && r.URL.Query().Get("page") == "1" {
						_, _ = fmt.Fprint(w, `{"entries":[],"total_pages":2}`)
						return
					}
					_, _ = fmt.Fprintf(w, `{"entries":[{"info_hash":"%s"}],"total_pages":2}`, hash)
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := New(Options{Decypharr: decypharr.NewClient(server.URL, "", ""), CSI: csi.NewChecker("")})
			ready, err := c.rearmReady(context.Background(), model.MediaCacheState{SymlinkPath: path}, hash)
			if mode == "library_error" {
				if err == nil {
					t.Fatal("authentication error hidden")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := mode == "removed_ready" || mode == "paginated"
			if ready != want {
				t.Fatalf("ready=%v want=%v", ready, want)
			}
		})
	}
}
