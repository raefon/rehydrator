package health

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestGatewayReadinessDoesNotRequireController(t *testing.T) {
	calls := 0
	s := NewAPIServer(APIOptions{Readiness: func(context.Context) error { calls++; return nil }})
	r := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	s = NewAPIServer(APIOptions{Readiness: func(context.Context) error { return fmt.Errorf("database unavailable") }})
	w = httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
