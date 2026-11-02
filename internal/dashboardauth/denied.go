package dashboardauth

import (
	_ "embed"
	"net/http"
)

//go:embed denied.html
var deniedPage []byte

func denyAccount(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(deniedPage)
}
