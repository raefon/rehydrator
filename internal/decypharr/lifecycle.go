package decypharr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

// PrepareTorrent resolves an indexer URL once so the exact torrent can be saved
// before pruning. Redirects to magnets retain the full resolved URI.
func (c *Client) PrepareTorrent(ctx context.Context, t model.TorrentMetadata) (model.TorrentMetadata, error) {
	// Capture may receive an external payload before its type is known. Mixed
	// payloads are rejected instead of silently selecting one of them.
	if t.TorrentURL != "" {
		if t.Magnet != "" || len(t.TorrentFile) > 0 || t.TorrentFilename != "" {
			return t, fmt.Errorf("download URL cannot be combined with another torrent source")
		}
		if t.Kind != "" && t.Kind != model.SourceUnresolved && t.Kind != model.SourceTorrentFile {
			return t, fmt.Errorf("URL conflicts with source kind")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t.TorrentURL)), "magnet:") {
			t.Magnet = t.TorrentURL
			t.TorrentURL = ""
			t.Kind = model.SourceMagnet
		} else {
			data, name, err := c.fetchTorrentFile(ctx, t.TorrentURL, t.SourceTitle)
			if redirect, ok := err.(*magnetRedirect); ok {
				t.Magnet = redirect.uri
				t.Kind = model.SourceMagnet
				t.TorrentURL = ""
			} else if err != nil {
				return t, err
			} else {
				t.TorrentFile = data
				t.TorrentFilename = name
				t.TorrentURL = ""
				t.Kind = model.SourceTorrentFile
			}
		}
	} else if t.Kind == "" || t.Kind == model.SourceUnresolved {
		switch {
		case len(t.TorrentFile) > 0 && t.Magnet == "":
			t.Kind = model.SourceTorrentFile
		case t.Magnet != "" && len(t.TorrentFile) == 0:
			t.Kind = model.SourceMagnet
		default:
			return t, model.ErrSourceRecoveryRequired
		}
	}
	return t.ValidateSource()
}

// PruneTorrent uses the provider-aware browse endpoint. qBittorrent deletion
// alone only clears the active queue. deleteFiles=false preserves local links.
func (c *Client) PruneTorrent(ctx context.Context, hash string) error {
	hash, err := model.NormalizeInfoHash(hash)
	if err != nil {
		return err
	}
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/api/browse/torrents/"+url.PathEscape(hash), nil)
	if err != nil {
		return err
	}
	raw, status, err := c.do(req)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Decypharr provider prune failed: status=%d body=%s", status, raw)
	}
	// Clear the old completed queue entry so the saved source can be re-added.
	return c.DeleteTorrent(ctx, hash, false)
}

func (t *TorrentInfo) Ready() bool {
	if t == nil || t.Progress < 1 {
		return false
	}
	switch strings.ToLower(t.State) {
	case "pausedup", "stalledup", "uploading", "forcedup", "stoppedup":
		return true
	}
	return false
}

// LibraryHasTorrent checks the retained library rather than the completed queue.
func (c *Client) LibraryHasTorrent(ctx context.Context, hash string) (bool, error) {
	hash, err := model.NormalizeInfoHash(hash)
	if err != nil {
		return false, err
	}
	if err := c.authenticate(ctx); err != nil {
		return false, err
	}
	for page := 1; ; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/browse/__all__?limit=100&page=%d", c.base, page), nil)
		if err != nil {
			return false, err
		}
		raw, status, err := c.do(req)
		if err != nil {
			return false, err
		}
		if status < 200 || status >= 300 {
			return false, fmt.Errorf("Decypharr library lookup failed: status=%d", status)
		}
		var result struct {
			Entries []struct {
				InfoHash string `json:"info_hash"`
			}
			TotalPages int `json:"total_pages"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return false, fmt.Errorf("invalid Decypharr library response: %w", err)
		}
		if result.Entries == nil {
			return false, fmt.Errorf("Decypharr library response missing entries")
		}
		for _, entry := range result.Entries {
			if strings.EqualFold(entry.InfoHash, hash) {
				return true, nil
			}
		}
		if page >= result.TotalPages {
			return false, nil
		}
	}
}
