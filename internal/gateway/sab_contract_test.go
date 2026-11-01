package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/raefon/rehydrator/internal/config"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the query/multipart shapes used by Arr's SAB client, including
// completion only after the separately mounted controller verifies the payload.
func TestHostedNZBSABClientContracts(t *testing.T) {
	for _, category := range []string{"sonarr", "radarr"} {
		t.Run(category, func(t *testing.T) {
			s, p, conn := testService(t)
			s.direct = true
			s.SetRole(config.RoleGateway)
			ctx := context.Background()
			get := func(values url.Values) map[string]json.RawMessage {
				t.Helper()
				values.Set("apikey", s.cfg.SABAPIKey)
				r := httptest.NewRequest("GET", "/sabnzbd/api?"+values.Encode(), nil)
				w := httptest.NewRecorder()
				s.SAB(w, r)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				var result map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			raw := get(url.Values{"mode": {"version"}})
			var version string
			if err := json.Unmarshal(raw["version"], &version); err != nil || version == "" {
				t.Fatal(raw, err)
			}
			raw = get(url.Values{"mode": {"fullstatus"}, "skip_dashboard": {"1"}})
			var status struct {
				CompleteDir string `json:"completedir"`
			}
			if err := json.Unmarshal(raw["status"], &status); err != nil || status.CompleteDir != s.cfg.DownloadPath {
				t.Fatal(raw, err)
			}
			raw = get(url.Values{"mode": {"get_config"}})
			var configuration struct {
				Categories []struct{ Name, Dir string }
				Sorters    []json.RawMessage
				Misc       struct {
					CompleteDir  string `json:"complete_dir"`
					Retention    string `json:"history_retention_option"`
					TVSorting    bool   `json:"enable_tv_sorting"`
					MovieSorting bool   `json:"enable_movie_sorting"`
				}
			}
			if err := json.Unmarshal(raw["config"], &configuration); err != nil || configuration.Sorters == nil || len(configuration.Sorters) != 0 || configuration.Misc.Retention != "all" || configuration.Misc.TVSorting || configuration.Misc.MovieSorting {
				t.Fatal(configuration, err)
			}
			categoryRoot := ""
			for _, cat := range configuration.Categories {
				if cat.Name == category {
					categoryRoot = cat.Dir
				}
			}
			if categoryRoot != filepath.Join(s.cfg.DownloadPath, category) {
				t.Fatal("wrong category root", configuration)
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, err := writer.CreateFormFile("name", "original.nzb")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(file, testNZB)
			_ = writer.Close()
			values := url.Values{"mode": {"addfile"}, "apikey": {s.cfg.SABAPIKey}, "cat": {category}, "priority": {"0"}, "output": {"json"}}
			req := httptest.NewRequest("POST", "/sabnzbd/api?"+values.Encode(), &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()
			s.SAB(w, req)
			var added struct {
				Status bool
				IDs    []string `json:"nzo_ids"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &added) != nil || !added.Status || len(added.IDs) != 1 {
				t.Fatal(w.Code, w.Body.String())
			}
			id := added.IDs[0]
			type slot struct {
				ID                                   string `json:"nzo_id"`
				Status, Name, Storage, Category, Cat string
				Bytes                                int64
			}
			slots := func(mode string) []slot {
				t.Helper()
				raw := get(url.Values{"mode": {mode}, "category": {category}, "start": {"0"}, "limit": {"100"}})
				var response struct{ Slots []slot }
				if err := json.Unmarshal(raw[mode], &response); err != nil {
					t.Fatal(err)
				}
				return response.Slots
			}
			queue := slots("queue")
			if len(queue) != 1 || queue[0].ID != id || queue[0].Status != "Queued" || queue[0].Cat != category || len(slots("history")) != 0 {
				t.Fatal(queue)
			}
			source, err := s.Source(ctx, id)
			if err != nil || string(source.NZBFile) != testNZB || source.InfoHash != "" {
				t.Fatal("original NZB lost", source, err)
			}
			if err = s.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			p.ready = true
			due(t, s, conn, id)
			if err = s.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if len(slots("history")) != 0 {
				t.Fatal("provider readiness bypassed CSI verification")
			}
			j, err := s.repo.GatewayJob(ctx, s.tenant, id)
			if err != nil || j.State != "Mounting" {
				t.Fatal(j, err)
			}
			controller := NewDirect(config.GatewayConfig{CloudUsenetConfig: s.cfg}, s.tenant, s.repo, nil, nil, "sonarr", "radarr")
			controller.SetRole(config.RoleController)
			mounted := filepath.Join(s.cfg.MountPath, "usenet", id, "Release", "Movie & test.mkv")
			if err = os.MkdirAll(filepath.Dir(mounted), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(mounted, []byte("abcdef"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = controller.ReconcileMount(ctx); err != nil {
				t.Fatal(err)
			}
			history := slots("history")
			if len(history) != 1 || history[0].ID != id || history[0].Status != "Completed" || history[0].Category != category || history[0].Bytes != 6 || len(slots("queue")) != 0 {
				t.Fatal(history)
			}
			// Arr imports from history.storage, not the category base directory.
			storage := history[0].Storage
			relative, err := filepath.Rel(categoryRoot, storage)
			if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				t.Fatal("invalid import root", storage, categoryRoot, err)
			}
			staged := filepath.Join(storage, "Release", "Movie & test.mkv")
			if target, err := os.Readlink(staged); err != nil || target != mounted {
				t.Fatal("history does not point at staged media", target, err)
			}
			get(url.Values{"mode": {"history"}, "name": {"delete"}, "value": {id}, "del_files": {"0"}, "archive": {"1"}})
			if len(slots("history")) != 0 {
				t.Fatal("history cleanup did not hide job")
			}
			if _, err = s.Source(ctx, id); err != nil {
				t.Fatal("history cleanup lost NZB", err)
			}
			req = httptest.NewRequest("HEAD", "/webdav/usenet/"+id+"/Release/Movie%20%26%20test.mkv", nil)
			req.SetBasicAuth(s.cfg.DAVUsername, s.cfg.DAVPassword)
			w = httptest.NewRecorder()
			s.DAV(w, req)
			if w.Code != 200 || p.links != 0 || p.creates != 1 {
				t.Fatal("history cleanup removed catalog or caused extra provider work", w.Code, p.links, p.creates)
			}
		})
	}
}
