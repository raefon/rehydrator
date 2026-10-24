package csi

import (
	"context"
	"errors"
	"sync"
)

var ErrProbeBusy = errors.New("filesystem probe slots are occupied")
var ErrProbePathBusy = errors.New("a filesystem probe for this path is still running")

// ReadProbe bounds outstanding filesystem work even when FUSE ignores deadlines.
// Timed-out reads retain their slots until they return; paths are never duplicated.
type ReadProbe struct {
	slots  chan struct{}
	read   func(string) bool
	mu     sync.Mutex
	active map[string]bool
}

func NewReadProbe(checker *Checker) *ReadProbe { return NewReadProbeWithLimit(checker, 1) }
func NewReadProbeWithLimit(checker *Checker, limit int) *ReadProbe {
	if limit < 1 {
		limit = 1
	}
	return &ReadProbe{slots: make(chan struct{}, limit), read: checker.Readable}
}
func (p *ReadProbe) Check(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.mu.Lock()
	if p.active[path] {
		p.mu.Unlock()
		return false, ErrProbePathBusy
	}
	select {
	case p.slots <- struct{}{}:
	default:
		p.mu.Unlock()
		return false, ErrProbeBusy
	}
	if p.active == nil {
		p.active = make(map[string]bool)
	}
	p.active[path] = true
	p.mu.Unlock()
	done := make(chan bool, 1)
	go func() {
		result := p.read(path)
		p.mu.Lock()
		delete(p.active, path)
		<-p.slots
		p.mu.Unlock()
		done <- result
	}()
	select {
	case result := <-done:
		return result, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
