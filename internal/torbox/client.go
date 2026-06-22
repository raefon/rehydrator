package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/raefon/rehydrator/internal/httpx"
)

const defaultBaseURL = "https://api.torbox.app/v1/api"

type Client struct {
	key  string
	base string
	http *http.Client
}

type Torrent struct {
	ID       string
	InfoHash string
	Name     string
	Raw      map[string]any
}

type apiResponse struct {
	Success bool   `json:"success"`
	Error   any    `json:"error"`
	Detail  string `json:"detail"`
	Data    any    `json:"data"`
}

func NewClient(key string) *Client {
	return &Client{
		key:  strings.TrimSpace(key),
		base: defaultBaseURL,
		http: httpx.DefaultClient(),
	}
}

func (c *Client) Configured() bool {
	return strings.TrimSpace(c.key) != ""
}

func (c *Client) FindTorrentByHash(ctx context.Context, infoHash string) (Torrent, bool, error) {
	infoHash = cleanHash(infoHash)
	if infoHash == "" {
		return Torrent{}, false, fmt.Errorf("missing infohash")
	}
	if !c.Configured() {
		return Torrent{}, false, fmt.Errorf("missing TorBox API key")
	}

	torrents, err := c.ListTorrents(ctx)
	if err != nil {
		return Torrent{}, false, err
	}

	for _, torrent := range torrents {
		if cleanHash(torrent.InfoHash) == infoHash {
			return torrent, true, nil
		}
	}

	return Torrent{}, false, nil
}

func (c *Client) ListTorrents(ctx context.Context) ([]Torrent, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("missing TorBox API key")
	}

	q := url.Values{}
	q.Set("bypass_cache", "true")
	q.Set("limit", "1000")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/torrents/mylist?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)

	slog.Info("torbox mylist request", "url", req.URL.String())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		// TorBox can return 404 for empty list/id-style requests. Treat as empty list.
		slog.Warn("torbox mylist returned 404; treating as empty list", "body", string(raw))
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("torbox mylist unexpected status: %s body=%s", resp.Status, string(raw))
	}

	var decoded apiResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode torbox mylist response: %w body=%s", err, string(raw))
	}
	if !decoded.Success && decoded.Error != nil {
		return nil, fmt.Errorf("torbox mylist failed: error=%v detail=%s", decoded.Error, decoded.Detail)
	}

	torrents := parseTorrentList(decoded.Data)
	slog.Info("torbox mylist response parsed", "count", len(torrents))
	return torrents, nil
}

func (c *Client) DeleteTorrent(ctx context.Context, torrentID string) error {
	torrentID = strings.TrimSpace(torrentID)
	if torrentID == "" {
		return fmt.Errorf("missing TorBox torrent ID")
	}
	if !c.Configured() {
		return fmt.Errorf("missing TorBox API key")
	}

	torrentIDValue := any(torrentID)
	if idInt, err := strconv.Atoi(torrentID); err == nil {
		torrentIDValue = idInt
	}

	payload := map[string]any{
		"torrent_id": torrentIDValue,
		"operation":  "delete",
		"all":        false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/torrents/controltorrent", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	slog.Info("torbox controltorrent delete request", "url", req.URL.String(), "torrent_id", torrentID)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("torbox delete unexpected status: %s body=%s", resp.Status, string(body))
	}

	var decoded apiResponse
	if err := json.Unmarshal(body, &decoded); err == nil {
		if !decoded.Success && decoded.Error != nil {
			return fmt.Errorf("torbox delete failed: error=%v detail=%s", decoded.Error, decoded.Detail)
		}
	}

	slog.Info("torbox controltorrent delete response", "torrent_id", torrentID, "body", string(body))
	return nil
}

func parseTorrentList(data any) []Torrent {
	items := make([]Torrent, 0)
	for _, m := range dataMaps(data) {
		id := firstNonEmptyAny(m, "id", "torrent_id", "torrentId", "torrentID")
		infoHash := cleanHash(firstNonEmptyAny(m,
			"hash", "infohash", "infoHash", "info_hash", "torrent_hash", "torrentHash",
			"btih", "magnet_hash", "cached_hash",
		))

		// Some responses may nest useful identifiers in child fields.
		if infoHash == "" {
			infoHash = cleanHash(findHashRecursive(m))
		}
		if id == "" {
			id = findIDRecursive(m)
		}

		items = append(items, Torrent{
			ID:       id,
			InfoHash: infoHash,
			Name:     firstNonEmptyAny(m, "name", "torrent_name", "title"),
			Raw:      m,
		})
	}
	return items
}

func dataMaps(data any) []map[string]any {
	switch v := data.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		// mylist may return an object when filtered by ID, or a keyed map of objects.
		if looksLikeTorrent(v) {
			return []map[string]any{v}
		}
		out := make([]map[string]any, 0)
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func looksLikeTorrent(m map[string]any) bool {
	for _, key := range []string{"id", "torrent_id", "torrentId", "hash", "infohash", "infoHash", "name"} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func firstNonEmptyAny(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s := anyToString(v); strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func findHashRecursive(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if s := firstNonEmptyAny(t, "hash", "infohash", "infoHash", "info_hash", "torrent_hash", "torrentHash", "btih"); cleanHash(s) != "" {
			return s
		}
		for _, child := range t {
			if s := findHashRecursive(child); cleanHash(s) != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := findHashRecursive(child); cleanHash(s) != "" {
				return s
			}
		}
	case string:
		return parseHashFromString(t)
	}
	return ""
}

func findIDRecursive(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if s := firstNonEmptyAny(t, "id", "torrent_id", "torrentId", "torrentID"); s != "" {
			return s
		}
		for _, child := range t {
			if s := findIDRecursive(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := findIDRecursive(child); s != "" {
				return s
			}
		}
	}
	return ""
}

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func cleanHash(s string) string {
	s = parseHashFromString(s)
	if len(s) != 40 {
		return ""
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return s
}

func parseHashFromString(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "urn:btih:")
	if strings.HasPrefix(s, "magnet:") {
		u, err := url.Parse(s)
		if err == nil {
			for _, xt := range u.Query()["xt"] {
				xt = strings.TrimSpace(strings.ToLower(xt))
				if strings.HasPrefix(xt, "urn:btih:") {
					return strings.TrimPrefix(xt, "urn:btih:")
				}
			}
		}
	}

	// Pull the first 40-char hex infohash from strings like "...373f...".
	for i := 0; i+40 <= len(s); i++ {
		candidate := s[i : i+40]
		ok := true
		for _, r := range candidate {
			if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
				ok = false
				break
			}
		}
		if ok {
			return candidate
		}
	}
	return s
}
