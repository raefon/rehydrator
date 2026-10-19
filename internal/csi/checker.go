package csi

import (
	"os"
	"path/filepath"
	"strings"
)

type Checker struct {
	base string
}

func NewChecker(base string) *Checker {
	return &Checker{base: base}
}

func (c *Checker) Exists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	candidates := []string{path}

	if c.base != "" {
		clean := strings.TrimPrefix(path, c.base)
		clean = strings.TrimPrefix(clean, "/")
		candidates = append(candidates, filepath.Join(c.base, clean))
	}

	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}

	return false
}

// Readable requires file data, rather than a potentially stale directory entry.
func (c *Checker) Readable(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	candidates := []string{path}
	if c.base != "" {
		clean := strings.TrimPrefix(strings.TrimPrefix(path, c.base), "/")
		candidates = append(candidates, filepath.Join(c.base, clean))
	}
	for _, candidate := range candidates {
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}
		var b [1]byte
		n, err := f.Read(b[:])
		f.Close()
		if n == 1 && err == nil {
			return true
		}
	}
	return false
}
