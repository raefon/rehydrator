package csi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMissingPathProbeDoesNotOpenFiles(t *testing.T) {
	dir := t.TempDir()
	probe := NewMissingPathProbe()
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if missing, err := probe.Check(context.Background(), f); err != nil || missing {
		t.Fatal("existing file marked missing", err)
	}
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "absent"), broken); err != nil {
		t.Fatal(err)
	}
	if missing, err := probe.Check(context.Background(), broken); err != nil || !missing {
		t.Fatal("dangling symlink not detected", err)
	}
	// A directory is stat-able; probing it must not attempt a content read.
	if missing, err := probe.Check(context.Background(), dir); err != nil || missing {
		t.Fatal("directory marked missing", err)
	}
}
