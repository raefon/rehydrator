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
