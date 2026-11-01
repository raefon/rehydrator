package health

import (
	"github.com/raefon/rehydrator/internal/db"
	"net/http/httptest"
	"testing"
)

func TestRetiredRoutesAreNotMounted(t *testing.T) {
	server := NewAPIServer(APIOptions{Repo: &db.Repo{}, Token: "secret", RequireToken: true})
	for _, path := range []string{"/api/prune/movie/1", "/api/prune/series/1", "/api/health/torbox", "/sabnzbd-cloud/api", "/cloud-usenet/dav/"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		server.srv.Handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
}
