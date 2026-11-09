package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/uilogs"
)

func TestDashboardSharedLogs(t *testing.T) {
	s, _, conn := testService(t)
	ctx := context.Background()
	s.dashboardExternalAuth = true
	t.Cleanup(func() {
		conn.Exec(ctx, "DELETE FROM ui_log_snapshots WHERE tenant=$1 OR tenant=$2", s.tenant, s.tenant+"-other")
	})
	publish := func(tenant, instance, source, timestamp string, count int) {
		t.Helper()
		entries := make([]uilogs.Entry, count)
		for i := range entries {
			entries[i] = uilogs.Entry{Source: source, Time: timestamp, Level: "INFO", Message: fmt.Sprintf("entry %d", i), Fields: map[string]string{}}
		}
		if err := s.repo.PublishLogs(ctx, tenant, instance, source, entries); err != nil {
			t.Fatal(err)
		}
	}
	publish(s.tenant, "gateway", "Gateway", "2026-11-09T12:00:00Z", 300)
	publish(s.tenant, "core", "Core", "2026-11-09T12:00:00.1Z", 300)
	publish(s.tenant+"-other", "other", "PRIVATE", "2026-11-09T12:00:01Z", 1)
	publish(s.tenant, "stale", "STALE", "2026-11-09T12:00:01Z", 1)
	if _, err := conn.Exec(ctx, "UPDATE ui_log_snapshots SET updated_at=now()-interval '1 minute' WHERE tenant=$1 AND instance='stale'", s.tenant); err != nil {
		t.Fatal(err)
	}
	request := func() struct {
		Entries []uilogs.Entry
		Sources []string
	} { t.Helper(); w := httptest.NewRecorder(); s.Dashboard(w, httptest.NewRequest("GET", "/dashboard/api/logs", nil)); if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}; var result struct {
		Entries []uilogs.Entry
		Sources []string
	}; if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}; return result }
	result := request()
	if len(result.Entries) != 600 || len(result.Sources) != 2 || result.Entries[0].Source != "Gateway" || result.Entries[599].Source != "Core" {
		t.Fatalf("incorrect aggregation: %d entries, %v", len(result.Entries), result.Sources)
	}
	// Refreshing a snapshot replaces it instead of duplicating its entries.
	publish(s.tenant, "gateway", "Gateway", "2026-11-09T12:00:02Z", 1)
	if rows := request().Entries; len(rows) != 301 || rows[len(rows)-1].Source != "Gateway" {
		t.Fatal("snapshot replacement or ordering failed")
	}
	// Multiple replicas cannot grow the returned feed beyond the window.
	publish(s.tenant, "gateway-2", "Gateway", "2026-11-09T12:00:03Z", 300)
	if len(request().Entries) != 600 {
		t.Fatal("unbounded feed")
	}
}
