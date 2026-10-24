package csi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadProbeBoundsBlockedWork(t *testing.T) {
	unblock := make(chan struct{})
	returned := make(chan struct{})
	p := &ReadProbe{slots: make(chan struct{}, 1), read: func(string) bool { <-unblock; close(returned); return true }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := p.Check(ctx, "file"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout not respected: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := p.Check(context.Background(), "file"); !errors.Is(err, ErrProbePathBusy) {
			t.Fatal("spawned another blocked read", err)
		}
	}
	close(unblock)
	<-returned
}

func TestReadProbeSequentialReads(t *testing.T) {
	p := &ReadProbe{slots: make(chan struct{}, 1), read: func(string) bool { return true }}
	for i := 0; i < 100; i++ {
		if ok, err := p.Check(context.Background(), "file"); err != nil || !ok {
			t.Fatalf("probe %d ok=%v err=%v", i, ok, err)
		}
	}
}

func TestReadProbeContinuesPastStalledPath(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	p := &ReadProbe{slots: make(chan struct{}, 2), read: func(path string) bool {
		if path == "slow" {
			<-unblock
		}
		return true
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := p.Check(ctx, "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := p.Check(context.Background(), "slow"); !errors.Is(err, ErrProbePathBusy) {
		t.Fatal("duplicated active path", err)
	}
	if ok, err := p.Check(context.Background(), "healthy"); err != nil || !ok {
		t.Fatal("healthy path blocked by unrelated timeout", err)
	}
}

func TestReadProbeCapsOutstandingPaths(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	p := &ReadProbe{slots: make(chan struct{}, 2), read: func(string) bool { <-unblock; return false }}
	for _, path := range []string{"first", "second"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := p.Check(ctx, path)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if _, err := p.Check(context.Background(), "third"); !errors.Is(err, ErrProbeBusy) {
		t.Fatal("read budget exceeded", err)
	}
}
