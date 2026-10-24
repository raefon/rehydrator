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
		if _, err := p.Check(context.Background(), "file"); !errors.Is(err, ErrProbeBusy) {
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
