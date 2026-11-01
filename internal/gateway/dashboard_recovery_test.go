package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

func TestRecoveryLookupCheckAndGuardedRearm(t *testing.T) {
	s, p, conn := testService(t)
	s.direct = true
	s.dashboardExternalAuth = true
	s.cfg.RecoverMissing = true
	ctx := context.Background()
	j := addTestJob(t, s)
	_, err := conn.Exec(ctx, `UPDATE gateway_jobs SET state='Missing',missing_checks=3,provider_id=101,attempts=1,hidden=true,next_check_at=now()+interval '1 hour' WHERE id=$1`, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, action, id string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/dashboard/api/recovery/"+action+"?id="+id, nil)
		r.Header.Set("X-Rehydrator-UI", "1")
		w := httptest.NewRecorder()
		s.Dashboard(w, r)
		return w
	}
	w := request("GET", "lookup", j.ID)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var detail recoveryDetail
	if err = json.Unmarshal(w.Body.Bytes(), &detail); err != nil || !detail.SourceRetained || !detail.RearmAvailable || !detail.Job.Hidden {
		t.Fatal(err, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "article@example") || strings.Contains(w.Body.String(), "dav-key") {
		t.Fatal("source or credentials exposed")
	}
	w = request("POST", "check", j.ID)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if after.Attempts != 1 || after.State != "Missing" || p.creates != 0 {
		t.Fatal("observation mutated job")
	}
	if w = request("POST", "check", j.ID); w.Code != 429 {
		t.Fatal("provider checks not bounded", w.Code)
	}
	w = request("POST", "rearm", j.ID)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if after.Attempts != 1 || after.ProviderID != 101 || after.State != "Missing" || p.creates != 0 {
		t.Fatal("manual request forced upload or reset budget")
	}
	// Existing worker checks absence again before queuing the saved source.
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if after.State != "Queued" || after.Attempts != 1 || p.creates != 0 {
		t.Fatal(after.State, after.Attempts, p.creates)
	}
	for _, state := range []string{"SubmissionUnknown", "Submitting", "Failed", "Completed"} {
		if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET state=$2 WHERE id=$1", j.ID, state); err != nil {
			t.Fatal(err)
		}
		if w = request("POST", "rearm", j.ID); w.Code != 409 {
			t.Fatal(state, w.Code)
		}
	}
	if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET state='Missing',attempts=3 WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", "rearm", j.ID); w.Code != 409 {
		t.Fatal("attempt budget bypassed")
	}
	other, err := s.repo.AddGatewaySource(ctx, s.tenant+"-other", "sonarr", model.TorrentMetadata{Protocol: "usenet", Kind: model.SourceNZBFile, NZBFile: []byte(testNZB), NZBFilename: "other.nzb"})
	if err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "lookup", other.ID); w.Code != 404 {
		t.Fatal("cross tenant disclosure", w.Code)
	}
	// Hidden history remains in the snapshot.
	r := httptest.NewRequest("GET", "/dashboard/api/snapshot", nil)
	w = httptest.NewRecorder()
	s.Dashboard(w, r)
	if !strings.Contains(w.Body.String(), j.ID) || !strings.Contains(w.Body.String(), `"hidden":true`) {
		t.Fatal("retained history omitted")
	}
}
func TestRecoveryTorrentHashLookup(t *testing.T) {
	s, _, _ := testService(t)
	s.direct = true
	s.dashboardExternalAuth = true
	hash := "0123456789012345678901234567890123456789"
	j, err := s.repo.AddGatewaySource(context.Background(), s.tenant, "sonarr", model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:" + hash})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/dashboard/api/recovery/lookup?id="+hash, nil)
	w := httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), j.ID) || strings.Contains(w.Body.String(), "magnet:?") {
		t.Fatal(w.Code, w.Body.String())
	}
}
