package seerr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/raefon/rehydrator/internal/httpx"
	"github.com/raefon/rehydrator/internal/model"
)

type Client struct {
	base string
	key  string
	http *http.Client
}

type Request struct {
	RequestKey string
	RequestID  int
	MediaType  model.MediaType
	TMDBID     int
	Title      string
	Status     string
	RawJSON    string
}

func NewClient(base, key string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: strings.TrimSpace(key), http: httpx.DefaultClient()}
}

func (c *Client) Configured() bool {
	return c != nil && c.base != "" && c.key != ""
}

func (c *Client) Requests(ctx context.Context, limit int) ([]Request, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("seerr client is not configured")
	}
	if limit <= 0 {
		limit = 100
	}

	q := url.Values{}
	q.Set("take", fmt.Sprintf("%d", limit))
	q.Set("skip", "0")
	q.Set("sort", "added")
	q.Set("sortDirection", "desc")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/request?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := httpx.CheckStatus(resp); err != nil {
		return nil, err
	}

	var decoded any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	entries := findRequestEntries(decoded)
	out := make([]Request, 0, len(entries))
	for _, entry := range entries {
		req, ok := parseRequest(entry)
		if ok {
			out = append(out, req)
		}
	}
	return out, nil
}

func findRequestEntries(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		return mapsFromSlice(t)
	case map[string]any:
		for _, key := range []string{"results", "items", "requests", "data"} {
			if raw, ok := t[key]; ok {
				switch r := raw.(type) {
				case []any:
					return mapsFromSlice(r)
				case map[string]any:
					for _, nested := range []string{"results", "items", "requests"} {
						if arr, ok := r[nested].([]any); ok {
							return mapsFromSlice(arr)
						}
					}
				}
			}
		}
	}
	return nil
}

func mapsFromSlice(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func parseRequest(m map[string]any) (Request, bool) {
	media := firstMap(m, "media", "mediaInfo")
	mediaType := normalizeMediaType(firstString(m, "mediaType", "type"))
	if mediaType == "" {
		mediaType = normalizeMediaType(firstString(media, "mediaType", "type"))
	}

	tmdbID := firstInt(m, "tmdbId", "tmdbID")
	if tmdbID == 0 {
		tmdbID = firstInt(media, "tmdbId", "tmdbID")
	}

	if mediaType == "" || tmdbID == 0 {
		return Request{}, false
	}

	requestID := firstInt(m, "id", "requestId")
	title := firstString(media, "title", "name", "originalTitle", "originalName")
	if title == "" {
		title = firstString(m, "title", "name", "subject")
	}
	status := firstString(m, "status", "requestStatus")
	if status == "" {
		status = firstString(media, "status", "requestStatus")
	}
	if status == "" {
		status = fmt.Sprint(firstAny(m, "status", "requestStatus"))
	}
	if status == "<nil>" {
		status = ""
	}

	raw, _ := json.Marshal(m)
	key := ""
	if requestID > 0 {
		key = fmt.Sprintf("request:%d", requestID)
	} else {
		key = fmt.Sprintf("%s:%d", mediaType, tmdbID)
	}

	return Request{
		RequestKey: key,
		RequestID:  requestID,
		MediaType:  mediaType,
		TMDBID:     tmdbID,
		Title:      title,
		Status:     status,
		RawJSON:    string(raw),
	}, true
}

func normalizeMediaType(s string) model.MediaType {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "media")
	s = strings.Trim(s, " _-")
	switch s {
	case "movie", "movies":
		return model.MediaMovie
	case "tv", "series", "show", "shows":
		return model.MediaSeries
	default:
		return ""
	}
}

func firstMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if v, ok := m[key].(map[string]any); ok {
			return v
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		case float64:
			return fmt.Sprintf("%.0f", t)
		case int:
			return fmt.Sprintf("%d", t)
		}
	}
	return ""
}

func firstAny(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func firstInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		case json.Number:
			i, _ := t.Int64()
			return int(i)
		case string:
			var i int
			_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &i)
			if i != 0 {
				return i
			}
		}
	}
	return 0
}
