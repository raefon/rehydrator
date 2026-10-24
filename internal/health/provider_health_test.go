package health

import (
	"github.com/raefon/rehydrator/internal/db"
	"net/http/httptest"
	"testing"
)

func TestTorBoxHealthReportRequiresAuth(t *testing.T) {
	server := NewAPIServer(APIOptions{Repo: &db.Repo{}, Token: "secret", RequireToken: true})
	for _, tc := range []struct {
		method, token string
		want          int
	}{{"GET", "", 401}, {"POST", "secret", 405}} {
		r := httptest.NewRequest(tc.method, "/api/health/torbox", nil)
		r.Header.Set("X-Rehydrator-Token", tc.token)
		w := httptest.NewRecorder()
		server.srv.Handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("method=%s code=%d", tc.method, w.Code)
		}
	}
}
