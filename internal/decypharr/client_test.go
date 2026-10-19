package decypharr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

func TestAddTorrentSources(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example&tr=udp%3A%2F%2Ftracker.example%3A80"
	for _, mode := range []string{"redirect", "redirect_chain", "file", "direct", "hash", "failure", "loop"} {
		t.Run(mode, func(t *testing.T) {
			adds := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/download":
					switch mode {
					case "redirect":
						w.Header().Set("Location", magnet)
						w.WriteHeader(302)
					case "redirect_chain":
						http.Redirect(w, r, "/magnet", 302)
					case "file":
						_, _ = io.WriteString(w, "d4:infod4:name7:exampleee")
					case "loop":
						http.Redirect(w, r, "/download", 302)
					default:
						w.WriteHeader(502)
					}
				case "/magnet":
					w.Header().Set("Location", magnet)
					w.WriteHeader(302)
				case "/api/v2/torrents/add":
					adds++
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("category") != "radarr" || r.FormValue("paused") != "false" {
						t.Error("missing add fields")
					}
					if mode == "file" {
						f, _, err := r.FormFile("torrents")
						if err != nil {
							t.Error(err)
							return
						}
						defer f.Close()
						data, _ := io.ReadAll(f)
						if string(data) != "d4:infod4:name7:exampleee" {
							t.Error("file changed")
						}
						if r.FormValue("urls") != "" {
							t.Error("unexpected magnet")
						}
					} else {
						want := magnet
						if mode == "hash" {
							want = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
						}
						if r.FormValue("urls") != want {
							t.Errorf("magnet got %q want %q", r.FormValue("urls"), want)
						}
					}
					_, _ = io.WriteString(w, "Ok.")
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := NewClient(server.URL, "", "")
			meta := model.TorrentMetadata{TorrentURL: server.URL + "/download", InfoHash: "0123456789012345678901234567890123456789"}
			if mode == "file" {
				meta.InfoHash = "7952549975b3256302ada821a1295c5a1ca628dc"
			}
			if mode == "direct" {
				meta.TorrentURL = magnet
			}
			if mode == "hash" {
				meta.TorrentURL = ""
				meta.Magnet = ""
			}
			prepared, err := c.PrepareTorrent(context.Background(), meta)
			if err == nil {
				_, err = c.AddTorrent(context.Background(), prepared, "radarr")
			}
			if mode == "failure" || mode == "loop" || mode == "hash" {
				if err == nil || adds != 0 {
					t.Fatalf("expected failure without add; adds=%d err=%v", adds, err)
				}
			} else if err != nil || adds != 1 {
				t.Fatalf("adds=%d err=%v", adds, err)
			}
			if c.http.CheckRedirect != nil {
				t.Error("shared redirect policy changed")
			}
		})
	}
}
