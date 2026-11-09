package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
)

type pruneProvider struct {
	fakeProvider
	state                    string
	found                    bool
	lookupError, deleteError error
	deletes                  int
}

func (p *pruneProvider) Get(context.Context, int64) (torbox.UsenetDownload, bool, error) {
	return torbox.UsenetDownload{State: p.state}, p.found, p.lookupError
}
func (p *pruneProvider) Delete(context.Context, int64) error { p.deletes++; return p.deleteError }
func TestPruneProviderGuards(t *testing.T) {
	for _, tc := range []struct {
		name, state              string
		found                    bool
		lookupError, deleteError error
		wantDelete, wantError    bool
	}{
		{name: "failed", state: "failed", found: true, wantDelete: true},
		{name: "already absent"},
		{name: "finished", state: "completed", found: true, wantDelete: true},
		{name: "downloading", state: "downloading", found: true, wantDelete: true},
		{name: "lookup failed", lookupError: errors.New("private"), wantError: true},
		{name: "delete failed", state: "failed", found: true, deleteError: errors.New("private"), wantDelete: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pruneProvider{state: tc.state, found: tc.found, lookupError: tc.lookupError, deleteError: tc.deleteError}
			s := &Service{provider: p}
			err := s.deletePlacement(context.Background(), db.GatewayJob{Protocol: "usenet", ProviderID: 42})
			if (err != nil) != tc.wantError || (p.deletes > 0) != tc.wantDelete {
				t.Fatalf("deletes=%d err=%v", p.deletes, err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("provider detail leaked")
			}
		})
	}
}
func TestPruneRequiresConfirmation(t *testing.T) {
	s := &Service{dashboardExternalAuth: true}
	for _, body := range []string{`{"ids":["id"]}`, `{"ids":[],"confirm":true}`, `{"ids":["id"],"confirm":true,"deleteAll":true}`} {
		req := httptest.NewRequest("POST", "/dashboard/api/jobs/prune", strings.NewReader(body))
		req.Header.Set("X-Rehydrator-UI", "1")
		w := httptest.NewRecorder()
		s.Dashboard(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func TestDashboardFailedPruneTransaction(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	s.dashboardExternalAuth = true
	j := addTestJob(t, s)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='Submitting' WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	var called bool
	reject := func(db.GatewayJob) error { called = true; return nil }
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, reject); err == nil || called {
		t.Fatal("in-flight submission accepted")
	}
	if err := s.repo.PruneGatewayJob(ctx, "other-tenant", j.ID, reject); err == nil || called {
		t.Fatal("cross tenant accepted")
	}
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='Failed' WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { return errors.New("provider unavailable") }); err == nil {
		t.Fatal("provider failure ignored")
	}
	if _, err := s.repo.GatewayJob(ctx, s.tenant, j.ID); err != nil {
		t.Fatal("failed job removed on error")
	}
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, reject); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.GatewayJob(ctx, s.tenant, j.ID); err == nil {
		t.Fatal("job retained")
	}
	var sourceCount int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM gateway_sources WHERE id=$1", j.SourceID).Scan(&sourceCount); err != nil || sourceCount != 1 {
		t.Fatal("original source was not retained", err)
	}
}

func TestPruneBlocksSharedProviderPlacement(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	other, err := s.repo.AddGatewayNZB(ctx, s.tenant, "sonarr", "other.nzb", []byte(testNZB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET state='Failed',provider_id=42 WHERE tenant=$1", s.tenant); err != nil {
		t.Fatal(err)
	}
	called := false
	err = s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("shared provider placement was deleted", other.ID)
	}
}
func TestDashboardLogsRequireAuth(t *testing.T) {
	s := &Service{qbUser: "arr", qbPassword: "secret"}
	r := httptest.NewRequest("GET", "/dashboard/api/logs", nil)
	w := httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.SetBasicAuth("arr", "secret")
	w = httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Current process only") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPruneBlocksLibraryLinkedJob(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='Failed' WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO media_cache_state(tenant,media_type,arr_id,symlink_path,state,source_protocol,source_download_id) VALUES($1,'movie',123,'/media/library.mkv','BROKEN','usenet',$2)`, s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", s.tenant) })
	called := false
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { called = true; return nil }); err == nil || called {
		t.Fatal("library linked job accepted")
	}
}
func TestDashboardPrunePartialResults(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	s.dashboardExternalAuth = true
	j := addTestJob(t, s)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='Failed' WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/dashboard/api/jobs/prune", strings.NewReader(`{"ids":["`+j.ID+`","missing","`+j.ID+`"],"confirm":true}`))
	r.Header.Set("X-Rehydrator-UI", "1")
	w := httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Results []struct {
			ID      string
			Deleted bool
			Error   string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 || !response.Results[0].Deleted || response.Results[1].Deleted || response.Results[1].Error == "" {
		t.Fatal(w.Body.String())
	}
}

func TestPruneSelectedNonFailedJobs(t *testing.T) {
	for _, state := range []string{"Queued", "Downloading", "Mounting", "Completed", "Missing"} {
		t.Run(state, func(t *testing.T) {
			s, _, conn := testService(t)
			ctx := context.Background()
			j := addTestJob(t, s)
			if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state=$3 WHERE tenant=$1 AND id=$2", s.tenant, j.ID, state); err != nil {
				t.Fatal(err)
			}
			called := false
			if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { called = true; return nil }); err != nil || !called {
				t.Fatal("selected job rejected", err)
			}
			if _, err := s.repo.GatewayJob(ctx, s.tenant, j.ID); err == nil {
				t.Fatal("selected job retained")
			}
		})
	}
}
func TestPruneUnresolvedSubmissionRetained(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='SubmissionUnknown',provider_id=0 WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { called = true; return nil }); err == nil || called {
		t.Fatal("unknown provider submission removed")
	}
}

func TestPruneBlocks4KLibraryLinkedJob(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET state='Failed' WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO media_cache_state(tenant,media_type,arr_id,symlink_path,state,source_protocol,source_download_id) VALUES($1,'movie',1,'/media/movies-4k/a.mkv','AVAILABLE','usenet',$2)`, s.tenant+":4k", j.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant=$1", s.tenant+":4k") })
	called := false
	if err := s.repo.PruneGatewayJob(ctx, s.tenant, j.ID, func(db.GatewayJob) error { called = true; return nil }); err == nil || called {
		t.Fatal("4k library reference was ignored")
	}
}
