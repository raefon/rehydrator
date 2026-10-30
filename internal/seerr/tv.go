package seerr

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Seasons requires explicit request scope. Missing scope never means all seasons.
func Seasons(raw any) []int {
	seen := map[int]bool{}
	var out []int
	add := func(v any) {
		n := -1
		switch x := v.(type) {
		case float64:
			if x == float64(int(x)) {
				n = int(x)
			}
		case int:
			n = x
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				n = parsed
			}
		}
		if n >= 0 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if entries, ok := raw.([]any); ok {
		for _, entry := range entries {
			if m, ok := entry.(map[string]any); ok {
				add(m["seasonNumber"])
			} else {
				add(entry)
			}
		}
	}
	return out
}
func DecodeRequest(raw []byte) (Request, bool) {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return Request{}, false
	}
	return parseRequest(v)
}
func Approved(status string) bool { return status == "2" || strings.EqualFold(status, "approved") }
