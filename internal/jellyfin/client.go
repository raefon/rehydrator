package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{strings.TrimRight(baseURL, "/"), apiKey, &http.Client{Timeout: timeout}}
}

// RefreshPath reports a changed file without scanning the entire library.
func (c *Client) RefreshPath(ctx context.Context, path string) error {
	if c == nil || c.baseURL == "" || c.apiKey == "" {
		return fmt.Errorf("jellyfin client is not configured")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("jellyfin refresh requires a path")
	}
	body, err := json.Marshal(map[string]any{"Updates": []map[string]string{{"Path": path, "UpdateType": "Modified"}}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/Library/Media/Updated", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jellyfin refresh failed: %s", resp.Status)
	}
	return nil
}

// ItemPath resolves Jellyfin's opaque item ID to its library file path.
func (c *Client) ItemPath(ctx context.Context, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("missing Jellyfin item ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Items/"+url.PathEscape(id), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jellyfin item lookup failed: %s", resp.Status)
	}
	var item struct{ Path string }
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return "", err
	}
	if strings.TrimSpace(item.Path) == "" {
		return "", fmt.Errorf("Jellyfin item has no file path")
	}
	return item.Path, nil
}

func (c *Client) Configured() bool { return c != nil && c.baseURL != "" && c.apiKey != "" }

func (c *Client) Ping(ctx context.Context) error {
	return c.request(ctx, http.MethodGet, "/System/Info")
}

func (c *Client) RefreshLibrary(ctx context.Context) error {
	return c.request(ctx, http.MethodPost, "/Library/Refresh")
}

func (c *Client) request(ctx context.Context, method, path string) error {
	if !c.Configured() {
		return fmt.Errorf("jellyfin client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Jellyfin request failed: %s", resp.Status)
	}
	return nil
}
