// Package uilogs keeps a bounded, process-local view of structured operational
// messages. Free-form attributes (errors, URLs, headers, payloads) are excluded.
package uilogs

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Source  string            `json:"source"`
	Time    string            `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
}

var Recent = &Buffer{}
var safeValue = regexp.MustCompile(`^[a-zA-Z0-9_.:/ -]{1,100}$`)
var urls = regexp.MustCompile(`(?i)(https?://|magnet:\?)\S+`)
var sensitive = regexp.MustCompile(`(?i)(password|secret|token|authorization|cookie|api.?key)\s*[:=]\s*\S+`)

func (b *Buffer) Snapshot() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]Entry, len(b.entries))
	copy(result, b.entries)
	return result
}

type Handler struct {
	base    slog.Handler
	buffer  *Buffer
	attrs   []slog.Attr
	grouped bool
}

func New(base slog.Handler) slog.Handler                          { return &Handler{base: base, buffer: Recent} }
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return h.base.Enabled(ctx, l) }
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	msg := sensitive.ReplaceAllString(urls.ReplaceAllString(r.Message, "[URL omitted]"), "[credential omitted]")
	if len(msg) > 512 {
		msg = msg[:512]
	}
	e := Entry{Time: r.Time.UTC().Format(time.RFC3339Nano), Level: r.Level.String(), Message: msg, Fields: map[string]string{}}
	add := func(a slog.Attr) {
		if h.grouped {
			return
		}
		switch a.Key {
		case "tenant", "job_id", "arr_id", "state", "protocol", "action", "role", "provider_id":
			v := a.Value.Resolve().String()
			if safeValue.MatchString(v) && !strings.Contains(v, "://") {
				e.Fields[a.Key] = v
			}
		}
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool { add(a); return true })
	h.buffer.mu.Lock()
	h.buffer.entries = append(h.buffer.entries, e)
	if len(h.buffer.entries) > 300 {
		h.buffer.entries = append([]Entry(nil), h.buffer.entries[len(h.buffer.entries)-300:]...)
	}
	h.buffer.mu.Unlock()
	return h.base.Handle(ctx, r)
}
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{base: h.base.WithAttrs(attrs), buffer: h.buffer, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...), grouped: h.grouped}
}
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{base: h.base.WithGroup(name), buffer: h.buffer, grouped: true}
}
