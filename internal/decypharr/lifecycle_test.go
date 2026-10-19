package decypharr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

const savedTorrent = "d4:infod4:name7:exampleee"
const savedHash = "7952549975b3256302ada821a1295c5a1ca628dc"

func TestTypedFileSubmissionSkipsIndexer(t *testing.T) {
	downloads, uploads := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old-indexer" {
			downloads++
			w.WriteHeader(410)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("API token missing")
		}
		if r.URL.Path != "/api/v2/torrents/add" {
			t.Error("unexpected API request")
			w.WriteHeader(404)
			return
		}
		uploads++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, header, err := r.FormFile("torrents")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != savedTorrent || header.Filename != "original.torrent" {
			t.Error("original file not preserved")
		}
		if r.FormValue("urls") != "" {
			t.Error("file submission must not include a magnet")
		}
		_, _ = fmt.Fprint(w, "Ok.")
	}))
	defer server.Close()
	c := NewClient(server.URL, "", "", "secret")
	result, err := c.AddTorrent(context.Background(), model.TorrentMetadata{Kind: model.SourceTorrentFile, InfoHash: savedHash, TorrentFile: []byte(savedTorrent), TorrentFilename: "original.torrent"}, "radarr")
	if err != nil || result.Hash != savedHash || uploads != 1 || downloads != 0 {
		t.Fatalf("result=%+v uploads=%d downloads=%d err=%v", result, uploads, downloads, err)
	}
}

func TestPrepareTorrentRejectsWrongFile(t *testing.T) {
	c := NewClient("http://unused", "", "")
	if _, err := c.PrepareTorrent(context.Background(), model.TorrentMetadata{TorrentFile: []byte(savedTorrent), InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err == nil {
		t.Fatal("wrong torrent accepted")
	}
	if _, err := c.PrepareTorrent(context.Background(), model.TorrentMetadata{TorrentFile: []byte("<html>indexer error</html>")}); err == nil {
		t.Fatal("HTML accepted as a torrent")
	}
}

func TestPruneThroughDecypharr(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			deleted, cleared := false, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("API token missing")
				}
				switch r.URL.Path {
				case "/api/browse/torrents/" + savedHash:
					if r.Method != "DELETE" {
						t.Error("expected DELETE")
					}
					if fail {
						w.WriteHeader(500)
						return
					}
					deleted = true
					_, _ = fmt.Fprint(w, `{"success":true}`)
				case "/api/v2/torrents/delete":
					if !deleted {
						t.Error("queue cleared before provider removal was accepted")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.FormValue("hashes") != savedHash || r.FormValue("deleteFiles") != "false" {
						t.Error("queue delete must preserve local links")
					}
					cleared = true
				default:
					t.Error("unexpected request")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := NewClient(server.URL, "", "", "secret")
			err := c.PruneTorrent(context.Background(), savedHash)
			if fail {
				if err == nil || cleared {
					t.Fatal("failed provider deletion considered successful")
				}
			} else if err != nil || !cleared {
				t.Fatalf("prune err=%v cleared=%v", err, cleared)
			}
		})
	}
}

func TestDecypharrReady(t *testing.T) {
	for _, tc := range []struct {
		state    string
		progress float64
		ready    bool
	}{
		{"pausedUP", 1, true}, {"uploading", 1, true}, {"stalledUP", 1, true}, {"downloading", 0.5, false}, {"error", 1, false}, {"missingFiles", 1, false}, {"pausedDL", 1, false},
	} {
		if (&TorrentInfo{State: tc.state, Progress: tc.progress}).Ready() != tc.ready {
			t.Errorf("incorrect readiness for %+v", tc)
		}
	}
	var missing *TorrentInfo
	if missing.Ready() {
		t.Fatal("missing queue item is not ready")
	}
}

func TestCaptureFileOnceThenRearmOffline(t *testing.T) {
	fetches := 0
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if r.Header.Get("Authorization") != "" {
			t.Error("Decypharr token leaked to indexer")
		}
		_, _ = io.WriteString(w, savedTorrent)
	}))
	c := NewClient("http://unused", "", "", "secret")
	source, err := c.PrepareTorrent(context.Background(), model.TorrentMetadata{InfoHash: savedHash, TorrentURL: indexer.URL + "/release.torrent"})
	indexer.Close()
	if err != nil || source.TorrentURL != "" || string(source.TorrentFile) != savedTorrent || fetches != 1 {
		t.Fatalf("source=%+v fetches=%d err=%v", source, fetches, err)
	}
	// Preparing a persisted source cannot revisit the now-unavailable indexer.
	if _, err := c.PrepareTorrent(context.Background(), source); err != nil {
		t.Fatal(err)
	}
}

func TestTorrentInfoMatchesRequestedHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hashes") != savedHash {
			t.Error("missing hash filter")
		}
		_, _ = fmt.Fprintf(w, `[{"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","progress":1,"state":"pausedUP"},{"hash":"%s","progress":0.5,"state":"downloading","magnet_uri":"magnet:?xt=urn:btih:%s"}]`, savedHash, savedHash)
	}))
	defer server.Close()
	info, err := NewClient(server.URL, "", "").TorrentInfo(context.Background(), savedHash)
	if err != nil || info == nil || info.Hash != savedHash || info.Ready() {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestInvalidSourcesNeverReachDecypharr(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(500) }))
	defer server.Close()
	c := NewClient(server.URL, "user", "password")
	for _, source := range []model.TorrentMetadata{
		{InfoHash: savedHash},
		{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + savedHash, TorrentFile: []byte(savedTorrent)},
		{Kind: model.SourceTorrentFile, TorrentFile: []byte(savedTorrent), Magnet: "magnet:?xt=urn:btih:" + savedHash},
		{Kind: model.SourceTorrentFile, TorrentURL: "http://old-indexer/file"},
	} {
		if _, err := c.AddTorrent(context.Background(), source, "radarr"); err == nil {
			t.Fatal("invalid source submitted")
		}
	}
	if requests != 0 {
		t.Fatalf("invalid sources made %d HTTP requests", requests)
	}
}

func TestCaptureRejectsMixedExternalSources(t *testing.T) {
	c := NewClient("http://unused", "", "")
	if _, err := c.PrepareTorrent(context.Background(), model.TorrentMetadata{TorrentURL: "http://indexer/file", Magnet: "magnet:?xt=urn:btih:" + savedHash}); err == nil {
		t.Fatal("capture silently chose one of two sources")
	}
}
