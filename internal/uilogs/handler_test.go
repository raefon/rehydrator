package uilogs

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestBoundedSanitizedLogs(t *testing.T) {
	b := &Buffer{}
	h := &Handler{base: slog.NewTextHandler(io.Discard, nil), buffer: b}
	logger := slog.New(h).With("tenant", "tenet-alice", "api_key", "PRIVATE_KEY")
	for i := 0; i < 305; i++ {
		logger.Warn("provider check", "job_id", "torrent-123", "error", "PRIVATE_ERROR", "url", "https://secret.example/token")
	}
	rows := b.Snapshot()
	if len(rows) != 300 {
		t.Fatal(len(rows))
	}
	e := rows[0]
	if e.Fields["tenant"] != "tenet-alice" || e.Fields["job_id"] != "torrent-123" || len(e.Fields) != 2 {
		t.Fatal(e)
	}
	logger.Error("token=PRIVATE_TOKEN https://secret.example/path")
	for _, e := range b.Snapshot() {
		if strings.Contains(e.Message, "PRIVATE") {
			t.Fatal("credential leaked")
		}
	}
}
func TestConcurrentLogs(t *testing.T) {
	b := &Buffer{}
	logger := slog.New(&Handler{base: slog.NewTextHandler(io.Discard, nil), buffer: b})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				logger.InfoContext(context.Background(), "test")
				b.Snapshot()
			}
		}()
	}
	wg.Wait()
	if len(b.Snapshot()) != 300 {
		t.Fatal("incorrect retention")
	}
}
