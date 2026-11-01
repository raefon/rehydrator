package gateway

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raefon/rehydrator/internal/dashboardauth"
	"github.com/raefon/rehydrator/internal/db"
)

func TestDashboardAuthAndOrigin(t *testing.T) {
	s := &Service{qbUser: "arr", qbPassword: "secret"}
	for _, tc := range []struct {
		name, method, origin, site, user, password, header string
		external                                           bool
		want                                               int
	}{
		{name: "anonymous", method: "GET", want: 401},
		{name: "forged identity", method: "GET", want: 401},
		{name: "bad password", method: "GET", user: "arr", password: "wrong", want: 401},
		{name: "external auth", method: "GET", external: true, want: 404},
		{name: "basic auth", method: "GET", user: "arr", password: "secret", want: 404},
		{name: "cross origin external", method: "POST", external: true, origin: "https://evil.example", header: "1", want: 403},
		{name: "cross site", method: "POST", external: true, site: "cross-site", header: "1", want: 403},
		{name: "form csrf", method: "POST", external: true, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.dashboardExternalAuth = tc.external
			r := httptest.NewRequest(tc.method, "http://gateway/dashboard/api/unknown", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Rehydrator-UI", tc.header)
			r.Header.Set("X-authentik-username", "admin")
			if tc.user != "" {
				r.SetBasicAuth(tc.user, tc.password)
			}
			w := httptest.NewRecorder()
			s.Dashboard(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
}
func TestDashboardDurableUploadAndSnapshot(t *testing.T) {
	s, _, _ := testService(t)
	s.dashboardExternalAuth = true
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("category", "sonarr")
	file, _ := form.CreateFormFile("upload", "original.nzb")
	_, _ = file.Write([]byte(testNZB))
	form.Close()
	r := httptest.NewRequest("POST", "/dashboard/api/jobs", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-Rehydrator-UI", "1")
	w := httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var job dashboardJob
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	stored, err := s.repo.GatewayJob(r.Context(), s.tenant, job.ID)
	if err != nil || string(stored.NZB) != testNZB {
		t.Fatal("original source not retained", err)
	}
	// The same service must not leak a different tenant's jobs or original source fields.
	other, err := s.repo.AddGatewayNZB(r.Context(), s.tenant+"-other", "sonarr", "other.nzb", []byte(testNZB))
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	r = httptest.NewRequest("GET", "/dashboard/api/snapshot", nil)
	w = httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "other.nzb") || strings.Contains(w.Body.String(), "article@example") || strings.Contains(w.Body.String(), "magnet") {
		t.Fatal(w.Code, w.Body.String())
	}
	var snapshot struct{ Jobs []dashboardJob }
	if err = json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || len(snapshot.Jobs) != 1 {
		t.Fatal(err, w.Body.String())
	}
}
func TestDashboardHidesStaleFiles(t *testing.T) {
	s := &Service{}
	j := db.GatewayJob{ID: "torrent-test", Protocol: "torrent", State: "Missing", Magnet: "secret"}
	got := s.dashboardJob(j)
	if len(got.Files) != 0 {
		t.Fatal("stale files published")
	}
}

func TestOIDCDoesNotFallBackToBasicOrExternal(t *testing.T) {
	s := &Service{dashboardOIDC: &dashboardauth.Auth{}, dashboardExternalAuth: true, qbUser: "arr", qbPassword: "secret"}
	r := httptest.NewRequest("GET", "/dashboard/api/snapshot", nil)
	r.SetBasicAuth("arr", "secret")
	r.Header.Set("X-authentik-username", "admin")
	w := httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 401 {
		t.Fatal("OIDC bypass", w.Code)
	}
	r = httptest.NewRequest("GET", "/dashboard/api/auth", nil)
	w = httptest.NewRecorder()
	s.Dashboard(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"oidc"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
