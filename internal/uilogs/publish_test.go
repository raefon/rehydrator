package uilogs

import (
	"context"
	"testing"
)

type testStore struct {
	cancel                   context.CancelFunc
	tenant, instance, source string
	entries                  []Entry
}

func (s *testStore) PublishLogs(_ context.Context, tenant, instance, source string, entries []Entry) error {
	s.tenant, s.instance, s.source, s.entries = tenant, instance, source, entries
	s.cancel()
	return nil
}
func TestPublishSourceAndCancellation(t *testing.T) {
	previous := Recent
	Recent = &Buffer{entries: []Entry{{Message: "sanitized message"}}}
	defer func() { Recent = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &testStore{cancel: cancel}
	Publish(ctx, store, "tenant-a", "Core")
	if store.tenant != "tenant-a" || store.instance == "" || store.source != "Core" || len(store.entries) != 1 || store.entries[0].Source != "Core" {
		t.Fatalf("unexpected snapshot: %+v", store)
	}
	if Recent.Snapshot()[0].Source != "" {
		t.Fatal("publisher mutated local buffer")
	}
}
