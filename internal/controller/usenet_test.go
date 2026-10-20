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

func TestUsenetReadiness(t *testing.T) {
	for _, mode := range []string{"complete", "downloading", "failed", "removed-retained", "removed-missing", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "episode.mkv")
			if mode != "unreadable" {
				if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sabnzbd/api":
					_ = r.ParseForm()
					kind := r.FormValue("mode")
					if r.FormValue("nzo_ids") != "opaque-job" {
						t.Error("incorrect runtime identity")
					}
					slots := "[]"
					if kind == "history" && (mode == "complete" || mode == "unreadable") {
						slots = `[{"nzo_id":"opaque-job","status":"Completed"}]`
					}
					if kind == "history" && mode == "failed" {
						slots = `[{"nzo_id":"opaque-job","status":"Failed"}]`
					}
					if kind == "queue" && mode == "downloading" {
						slots = `[{"nzo_id":"opaque-job","status":"Downloading"}]`
					}
					_, _ = fmt.Fprintf(w, `{"%s":{"slots":%s}}`, kind, slots)
				case "/api/browse/__all__":
					if mode == "removed-retained" {
						_, _ = fmt.Fprint(w, `{"entries":[{"info_hash":"opaque-job"}],"total_pages":1}`)
					} else {
						_, _ = fmt.Fprint(w, `{"entries":[],"total_pages":1}`)
					}
				default:
					t.Errorf("unexpected torrent API: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := New(Options{Decypharr: decypharr.NewClient(server.URL, "", ""), CSI: csi.NewChecker("")})
			ready, err := c.rearmReady(context.Background(), model.MediaCacheState{SourceProtocol: model.ProtocolUsenet, DownloadJobID: "opaque-job", MediaType: model.MediaSeries, SymlinkPath: path}, "")
			if mode == "failed" {
				if err == nil {
					t.Fatal("failed job accepted")
				}
				return
			}
			if err != nil || ready != (mode == "complete" || mode == "removed-retained") {
				t.Fatalf("mode=%s ready=%v err=%v", mode, ready, err)
			}
		})
	}
}

func TestUsenetPruneNeverCallsProvider(t *testing.T) {
	c := New(Options{})
	c.handlePrune(context.Background(), model.MediaCacheState{SourceProtocol: model.ProtocolUsenet})
}
