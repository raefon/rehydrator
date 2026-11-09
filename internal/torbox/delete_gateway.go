package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Delete removes exactly one provider placement. No response bodies or credentials
// enter returned errors or operational logs.
func (c *UsenetClient) Delete(ctx context.Context, id int64) error {
	return c.deletePlacement(ctx, "usenet/controlusenetdownload", "usenet_id", id)
}
func (c *TorrentClient) Delete(ctx context.Context, id int64) error {
	return c.deletePlacement(ctx, "torrents/controltorrent", "torrent_id", id)
}
func (c *UsenetClient) deletePlacement(ctx context.Context, endpoint, key string, id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid provider ID")
	}
	body, _ := json.Marshal(map[string]any{key: id, "operation": "delete", "all": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid provider deletion request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("provider deletion request failed")
	}
	defer resp.Body.Close()
	var result struct{ Success bool }
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success {
		return fmt.Errorf("provider deletion was not confirmed (HTTP %d)", resp.StatusCode)
	}
	return nil
}
