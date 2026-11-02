package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/torbox"
)

type failingProvider struct {
	*fakeProvider
	state string
	err   error
}

func (p *failingProvider) Get(ctx context.Context, id int64) (torbox.UsenetDownload, bool, error) {
	present := false
	return torbox.UsenetDownload{ID: id, State: p.state, Present: &present}, true, p.err
}

func TestFailedNZBHistoryAllowsArrFailedDownloadHandling(t *testing.T) {
	for _, category := range []string{"sonarr", "radarr"} {
		t.Run(category, func(t *testing.T) {
			s, p, conn := testService(t)
			ctx := context.Background()
			s.provider = &failingProvider{fakeProvider: p, state: "failed (Aborted, cannot be completed - https://private.example/?apikey=secret)"}
			j, err := s.repo.AddGatewayNZB(ctx, s.tenant, category, "original.nzb", []byte(testNZB))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			due(t, s, conn, j.ID)
			if err = s.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			stored, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil || stored.State != "Failed" || stored.MissingChecks != 0 || p.creates != 1 {
				t.Fatal(stored.State, err)
			}
			call := func(mode string, extra url.Values) *httptest.ResponseRecorder {
				values := url.Values{"mode": {mode}, "apikey": {"sab-key"}, "cat": {category}}
				for key, value := range extra {
					values[key] = value
				}
				r := httptest.NewRequest("GET", SABPath+"?"+values.Encode(), nil)
				w := httptest.NewRecorder()
				s.SAB(w, r)
				return w
			}
			w := call("history", nil)
			var history struct {
				History struct {
					Slots []struct {
						ID               string `json:"nzo_id"`
						Status, Category string
						Message          string `json:"fail_message"`
					}
				}
			}
			if err = json.Unmarshal(w.Body.Bytes(), &history); err != nil {
				t.Fatal(err)
			}
			if len(history.History.Slots) != 1 {
				t.Fatal(w.Body.String())
			}
			entry := history.History.Slots[0]
			if entry.ID != j.ID || entry.Status != "Failed" || entry.Category != category || entry.Message == "" || strings.Contains(entry.Message, "secret") {
				t.Fatal(entry)
			}
			if strings.Contains(call("queue", nil).Body.String(), j.ID) {
				t.Fatal("failed download left in active queue")
			}
			due(t, s, conn, j.ID)
			if err = s.Reconcile(ctx); err != nil || p.creates != 1 {
				t.Fatal("failed source automatically reuploaded", err)
			}
			if w = call("history", url.Values{"name": {"delete"}, "value": {j.ID}, "del_files": {"1"}}); w.Code != 200 {
				t.Fatal(w.Code)
			}
			source, err := s.Source(ctx, j.ID)
			if err != nil || string(source.NZBFile) != testNZB {
				t.Fatal("failed history cleanup lost source", err)
			}
			stored, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil || !stored.Hidden || stored.State != "Failed" {
				t.Fatal("failed job not retained", err)
			}
		})
	}
}
func TestProviderRequestErrorDoesNotBlocklistRelease(t *testing.T) {
	s, p, conn := testService(t)
	ctx := context.Background()
	s.provider = &failingProvider{fakeProvider: p, err: errors.New("HTTP 429 temporary provider outage")}
	j := addTestJob(t, s)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	due(t, s, conn, j.ID)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || stored.State == "Failed" || stored.Attempts != 1 {
		t.Fatal(stored.State, err)
	}
}
