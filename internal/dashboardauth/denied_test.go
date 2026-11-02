package dashboardauth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeniedPage(t *testing.T) {
	w := httptest.NewRecorder()
	denyAccount(w)
	if w.Code != 403 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	body := w.Body.String()
	for _, required := range []string{`lang="en"`, `<title>Access denied`, `href="/auth/login"`, `<h1 id="title">`, `403 · Access denied`} {
		if !strings.Contains(body, required) {
			t.Fatal("missing", required)
		}
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "https://") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("denied page depends on external or unsafe resources")
	}
}
