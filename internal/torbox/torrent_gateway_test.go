package torbox

import (
	"context"
	"errors"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectTorrentAPIContract(t *testing.T) {
	for _, kind := range []model.SourceKind{model.SourceMagnet, model.SourceTorrentFile} {
		t.Run(string(kind), func(t *testing.T) {
			data := []byte("d4:infod6:lengthi6e4:name9:Movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789ee")
			source := model.TorrentMetadata{Kind: kind}
			if kind == model.SourceMagnet {
				source.Magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&tr=udp%3A%2F%2Ftracker.example%3A80"
			} else {
				source.TorrentFile = data
				source.TorrentFilename = "private.torrent"
			}
			adds := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer key" {
					t.Error("auth missing")
				}
				switch r.URL.Path {
				case "/torrents/mylist":
					if r.URL.Query().Get("id") == "" {
						fmt.Fprint(w, `{"success":true,"data":[]}`)
					} else {
						fmt.Fprint(w, `{"success":true,"data":[{"id":42,"download_state":"uploading","download_present":true,"files":[{"id":0,"name":"Movie.mkv","size":6}]}]}`)
					}
				case "/torrents/createtorrent":
					adds++
					if err := r.ParseMultipartForm(1024); err != nil {
						t.Error(err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("as_queued") != "false" {
						t.Error("submission explicitly requested provider queue")
					}
					if kind == model.SourceMagnet {
						if r.FormValue("magnet") != source.Magnet || len(r.MultipartForm.File) != 0 {
							t.Error("altered magnet")
						}
					} else {
						f, h, e := r.FormFile("file")
						if e != nil {
							t.Error(e)
							return
						}
						b, _ := io.ReadAll(f)
						f.Close()
						if string(b) != string(data) || h.Filename != "private.torrent" || r.FormValue("magnet") != "" {
							t.Error("altered metainfo")
						}
					}
					fmt.Fprint(w, `{"success":true,"data":{"torrent_id":42}}`)
				case "/torrents/requestdl":
					if r.URL.Query().Get("torrent_id") != "42" || r.URL.Query().Get("file_id") != "0" || r.URL.Query().Get("token") != "key" {
						t.Error("invalid download query")
					}
					fmt.Fprint(w, `{"success":true,"data":"https://cdn.example/movie"}`)
				default:
					t.Error(r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := NewTorrentClient("key")
			c.base = server.URL
			id, err := c.Submit(context.Background(), source)
			if err != nil || id != 42 || adds != 1 {
				t.Fatal(id, err, adds)
			}
			d, found, err := c.Get(context.Background(), id)
			if err != nil || !found || !d.Ready() {
				t.Fatal(d, found, err)
			}
			if _, err = c.Link(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTorrentCompletionRequiresProviderPresence(t *testing.T) {
	for _, present := range []string{"false", "null"} {
		t.Run(present, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"success":true,"data":[{"id":42,"download_state":"completed","download_present":%s,"files":[{"id":0,"name":"Movie.mkv","size":6}]}]}`, present)
			}))
			defer srv.Close()
			c := NewTorrentClient("key")
			c.base = srv.URL
			d, _, err := c.Get(context.Background(), 42)
			if err == nil && d.Ready() {
				t.Fatal("completed without presence")
			}
		})
	}
}

func TestQueuedTorrentIDIsNotAnActivePlacement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/mylist":
			fmt.Fprint(w, `{"success":true,"data":[]}`)
		case "/torrents/createtorrent":
			fmt.Fprint(w, `{"success":true,"data":{"queued_id":987,"torrent_id":null}}`)
		default:
			t.Error("unexpected request", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := NewTorrentClient("key")
	c.base = srv.URL
	id, err := c.Submit(context.Background(), model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"})
	if id != 0 || err == nil || !strings.Contains(err.Error(), "queued_id=987") {
		t.Fatal(id, err)
	}
	if errors.Is(err, ErrSubmissionNotStarted) {
		t.Fatal("accepted queued submission incorrectly marked safe to retry")
	}
}

func TestFindActiveIsReadOnlyAndMatchesExactAccountHash(t *testing.T) {
	wanted := "0123456789012345678901234567890123456789"
	for _, hash := range []string{wanted, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		t.Run(hash, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/torrents/mylist" {
					t.Error("lookup attempted mutation", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				if r.URL.Query().Get("offset") == "0" {
					fmt.Fprintf(w, `{"success":true,"data":[{"id":42,"hash":%q,"download_present":true,"download_state":"cached"}]}`, hash)
				} else {
					fmt.Fprint(w, `{"success":true,"data":[]}`)
				}
			}))
			defer srv.Close()
			c := NewTorrentClient("key")
			c.base = srv.URL
			id, err := c.FindActive(context.Background(), wanted)
			if err != nil {
				t.Fatal(err)
			}
			expected := int64(0)
			if hash == wanted {
				expected = 42
			}
			if id != expected {
				t.Fatal("wrong account placement adopted", id, expected)
			}
		})
	}
}
