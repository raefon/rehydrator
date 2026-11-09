package seerr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestEditionFromAuthoritativeLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/request/42" || r.Header.Get("X-Api-Key") != "test-key" {
			t.Error("invalid request")
		}
		_, _ = w.Write([]byte(`{"id":42,"is4k":true,"status":2,"media":{"mediaType":"movie","tmdbId":100}}`))
	}))
	defer server.Close()
	request, err := NewClient(server.URL, "test-key").RequestByID(context.Background(), 42)
	if err != nil || !request.Is4K || request.RequestID != 42 {
		t.Fatal(request, err)
	}
	standard, ok := DecodeRequest([]byte(`{"id":43,"status":2,"media":{"mediaType":"movie","tmdbId":100}}`))
	if !ok || standard.Is4K {
		t.Fatal(standard)
	}
}
