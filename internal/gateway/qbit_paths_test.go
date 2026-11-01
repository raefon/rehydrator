package gateway

import (
	"context"
	"encoding/json"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQbitImportPathsMeetSonarrContracts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []torbox.UsenetFile
	}{
		{"single file", []torbox.UsenetFile{{ID: 0, Name: "Show.S01E01.mkv", Size: 6}}},
		{"season pack", []torbox.UsenetFile{{ID: 0, Name: "Release/Season 01/Show.S01E01.mkv", Size: 6}, {ID: 1, Name: "Release/Season 01/Show.S01E02.mkv", Size: 6}}},
		{"loose files", []torbox.UsenetFile{{ID: 0, Name: "Show.S01E01.mkv", Size: 6}, {ID: 1, Name: "Show.S01E02.mkv", Size: 6}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := testService(t)
			s.qbUser = "arr"
			s.qbPassword = "secret"
			ctx := context.Background()
			j, err := s.repo.AddGatewaySource(ctx, s.tenant, "sonarr", model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: testMagnet})
			if err != nil {
				t.Fatal(err)
			}
			j.State = "Completed"
			j.ProviderID = 42
			j.Name = "Show.S01.Release"
			j.Files = tc.files
			if err = s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Hour); err != nil {
				t.Fatal(err)
			}
			if err = s.createLinks(j); err != nil {
				t.Fatal(err)
			}
			call := func(method, endpoint string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
				t.Helper()
				target := "/api/v2/" + endpoint
				body := values.Encode()
				if method == "GET" {
					target += "?" + body
					body = ""
				}
				r := httptest.NewRequest(method, target, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if cookie != nil {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				s.QBittorrent(w, r)
				if w.Code != 200 {
					t.Fatal(endpoint, w.Code, w.Body.String())
				}
				return w
			}
			login := call("POST", "auth/login", url.Values{"username": {"arr"}, "password": {"secret"}}, nil)
			cookie := login.Result().Cookies()[0]
			type torrent struct {
				SavePath    string  `json:"save_path"`
				ContentPath string  `json:"content_path"`
				Name        string  `json:"name"`
				Progress    float64 `json:"progress"`
			}
			var items []torrent
			w := call("GET", "torrents/info", nil, cookie)
			if err = json.Unmarshal(w.Body.Bytes(), &items); err != nil || len(items) != 1 {
				t.Fatal(w.Body.String(), err)
			}
			item := items[0]
			// Sonarr's >=2.6.1 client rejects completed items whose content_path equals
			// save_path. It also treats category savePath as a shared download root.
			var categories map[string]struct {
				SavePath string `json:"savePath"`
			}
			w = call("GET", "torrents/categories", nil, cookie)
			if err = json.Unmarshal(w.Body.Bytes(), &categories); err != nil {
				t.Fatal(err)
			}
			if item.SavePath == item.ContentPath || item.ContentPath == categories["sonarr"].SavePath {
				t.Fatal("Sonarr would reject base-directory import", item)
			}
			if item.SavePath != categories["sonarr"].SavePath || filepath.Dir(item.ContentPath) != item.SavePath || item.Progress != 1 || item.Name != j.Name {
				t.Fatal("inconsistent import metadata", item, categories)
			}
			var properties struct {
				SavePath string `json:"save_path"`
			}
			w = call("GET", "torrents/properties", url.Values{"hash": {j.InfoHash}}, cookie)
			if err = json.Unmarshal(w.Body.Bytes(), &properties); err != nil {
				t.Fatal(err)
			}
			var files []struct {
				Name string `json:"name"`
			}
			w = call("GET", "torrents/files", url.Values{"hash": {j.InfoHash}}, cookie)
			if err = json.Unmarshal(w.Body.Bytes(), &files); err != nil || len(files) != len(tc.files) {
				t.Fatal(w.Body.String(), err)
			}
			// Older Sonarr clients reconstruct import root from properties.save_path
			// and the top-level component of the first relative file name.
			legacyRoot := filepath.Join(properties.SavePath, strings.Split(files[0].Name, "/")[0])
			if legacyRoot != item.ContentPath {
				t.Fatal("legacy and modern import roots differ", legacyRoot, item.ContentPath)
			}
			for i, file := range files {
				path := filepath.Join(properties.SavePath, filepath.FromSlash(file.Name))
				relative, err := filepath.Rel(item.ContentPath, path)
				if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
					t.Fatal("file escaped import root", file.Name, err)
				}
				target, err := os.Readlink(path)
				expected := filepath.Join(s.cfg.MountPath, "torrents", j.ID, filepath.FromSlash(tc.files[i].Name))
				if err != nil || target != expected {
					t.Fatal("reported file is not the staged symlink", path, target, expected, err)
				}
			}
		})
	}
}
