package controller

import (
	"context"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileHealthOutageGuard(t *testing.T) {
	for _, tc := range []struct {
		name         string
		reads        []bool
		canary, want bool
	}{
		{"one missing no canary", []bool{false}, false, true},
		{"one missing with canary", []bool{false}, true, false},
		{"isolated failure", []bool{false, true, true}, false, false},
		{"widespread failure", []bool{false, false, true}, false, true},
		{"all failures", []bool{false, false, false}, true, true},
		{"healthy", []bool{true, true, true}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var results []db.FileHealthResult
			for _, read := range tc.reads {
				results = append(results, db.FileHealthResult{Readable: read})
			}
			if got := fileHealthOutage(results, tc.canary, 50); got != tc.want {
				t.Fatalf("outage=%v want=%v", got, tc.want)
			}
		})
	}
}

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
