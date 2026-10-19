package health

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSeerrTestNotification(t *testing.T) {
	// A nil repository ensures test notifications never touch persistence or rearm.
	h := apiHandler{token: "secret", requireToken: true}
	for _, key := range []string{"notification_type", "notificationType", "type"} {
		for _, authenticated := range []bool{false, true} {
			r := httptest.NewRequest("POST", "/api/seerr/webhook", strings.NewReader(`{"`+key+`":"TEST_NOTIFICATION","subject":"Test Notification"}`))
			if authenticated {
				r.Header.Set("X-Rehydrator-Token", "secret")
			}
			w := httptest.NewRecorder()
			h.handleSeerrWebhook(w, r)
			expected := 401
			if authenticated {
				expected = 200
			}
			if w.Code != expected {
				t.Fatalf("key=%s authenticated=%v: status=%d body=%s", key, authenticated, w.Code, w.Body.String())
			}
		}
	}
	r := httptest.NewRequest("POST", "/api/seerr/webhook", strings.NewReader(`{"notification_type":"MEDIA_APPROVED"}`))
	r.Header.Set("X-Rehydrator-Token", "secret")
	w := httptest.NewRecorder()
	h.handleSeerrWebhook(w, r)
	if w.Code != 400 {
		t.Fatalf("real notification without ID: status=%d", w.Code)
	}
}
