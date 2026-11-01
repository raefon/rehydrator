package controller

import (
	"context"
	"github.com/raefon/rehydrator/internal/csi"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRearmRequiresReadableBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{CSI: csi.NewChecker(""), CSIVisibilityTimeout: 10 * time.Millisecond, CSIVisibilityPoll: time.Millisecond})
	if c.waitForCSI(context.Background(), path) {
		t.Fatal("empty file metadata completed rearm")
	}
	if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	if !c.waitForCSI(context.Background(), path) {
		t.Fatal("readable file not accepted")
	}
}
