package csi

import (
	"context"
	"errors"
)

var ErrProbeBusy = errors.New("previous filesystem probe is still blocked")

// ReadProbe bounds caller wait and outstanding filesystem work. A stuck FUSE
// read keeps its slot until it returns; later sweeps cannot accumulate goroutines.
type ReadProbe struct {
	slots chan struct{}
	read  func(string) bool
}

func NewReadProbe(checker *Checker) *ReadProbe {
	return &ReadProbe{slots: make(chan struct{}, 1), read: checker.Readable}
}
func (p *ReadProbe) Check(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	select {
	case p.slots <- struct{}{}:
	default:
		return false, ErrProbeBusy
	}
	done := make(chan bool, 1)
	go func() { result := p.read(path); <-p.slots; done <- result }()
	select {
	case result := <-done:
		return result, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
