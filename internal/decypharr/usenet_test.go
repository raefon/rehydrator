package decypharr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

const nzbFixture = `<nzb><file><segments><segment bytes="1" number="1">article@example</segment></segments></file></nzb>`

func TestNZBCaptureAndSubmission(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/original.nzb" {
			w.Header().Set("Content-Disposition", `attachment; filename="Original.Release.nzb"`)
			_, _ = io.WriteString(w, nzbFixture)
			return
		}
		calls++
		if r.URL.Path != "/sabnzbd/api" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("mode") != "addfile" || r.FormValue("cat") != "sonarr" || r.FormValue("apikey") != "secret" {
			t.Errorf("incorrect SAB fields")
		}
		f, _, err := r.FormFile("name")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != nzbFixture {
			t.Error("NZB bytes changed")
		}
		_, _ = io.WriteString(w, `{"status":true,"nzo_ids":["new-opaque-job"]}`)
	}))
	defer server.Close()
	c := NewClient(server.URL, "", "", "secret")
	source, err := c.PrepareTorrent(context.Background(), model.TorrentMetadata{Protocol: model.ProtocolUsenet, TorrentURL: server.URL + "/original.nzb", DownloadID: "original-job"})
	if err != nil || source.Kind != model.SourceNZBFile || source.InfoHash != "" || source.NZBFilename != "Original.Release.nzb" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	id, err := c.AddNZB(context.Background(), source, "sonarr")
	if err != nil || id != "new-opaque-job" || calls != 1 {
		t.Fatalf("id=%s calls=%d err=%v", id, calls, err)
	}
	source.Magnet = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := c.AddNZB(context.Background(), source, "sonarr"); err == nil || calls != 1 {
		t.Fatal("mixed source reached network")
	}
}

func TestNZBStatus(t *testing.T) {
	for _, state := range []string{"Completed", "Failed", "Downloading", "missing", "bad-response"} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				mode := r.FormValue("mode")
				if r.FormValue("nzo_ids") != "opaque-job" {
					t.Error("wrong job ID")
				}
				if state == "bad-response" {
					_, _ = io.WriteString(w, `{"status":false}`)
					return
				}
				slots := []map[string]string{}
				if (mode == "history" && (state == "Completed" || state == "Failed")) || (mode == "queue" && state == "Downloading") {
					slots = append(slots, map[string]string{"nzo_id": "opaque-job", "status": state})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{mode: map[string]any{"slots": slots}})
			}))
			defer server.Close()
			ready, found, err := NewClient(server.URL, "", "").NZBStatus(context.Background(), "opaque-job", "sonarr")
			if state == "Failed" || state == "bad-response" {
				if err == nil {
					t.Fatal("failure hidden")
				}
				return
			}
			if err != nil || ready != (state == "Completed") || found != (state != "missing") {
				t.Fatal(fmt.Sprintf("ready=%v found=%v err=%v", ready, found, err))
			}
		})
	}
}
