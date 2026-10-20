package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	refreshMode string
	baseURL     string
	apiKey      string
	httpClient  *http.Client
}

func NewClient(baseURL, apiKey string, timeout time.Duration, modes ...string) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	mode := "notification"
	if len(modes) > 0 && modes[0] != "" {
		mode = modes[0]
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: &http.Client{Timeout: timeout}, refreshMode: mode}
}

// RefreshPath reports a changed file without scanning the entire library.
func (c *Client) RefreshPath(ctx context.Context, path string) error {
	if c == nil || c.baseURL == "" || c.apiKey == "" {
		return fmt.Errorf("jellyfin client is not configured")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("jellyfin refresh requires a path")
	}
	endpoint := "/Library/Media/Updated"
	var payload any = map[string]any{"Updates": []map[string]string{{"Path": path, "UpdateType": "Modified"}}}
	switch c.refreshMode {
	case "targeted_scan":
		endpoint = "/Library/ScanPath"
		payload = map[string]string{"Path": path}
	case "notification", "":
	default:
		return fmt.Errorf("unsupported Jellyfin refresh mode: %s", c.refreshMode)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", c.apiKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jellyfin refresh failed: %s", resp.Status)
	}
	if c.refreshMode == "targeted_scan" {
		var result struct {
			Status string
			ItemId string
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("invalid targeted scan response: %w", err)
		}
		if result.Status != "Created" && result.Status != "Refreshed" {
			return fmt.Errorf("Jellyfin targeted scan rejected path: status=%q", result.Status)
		}
		slog.Info("Jellyfin targeted scan accepted", "path", path, "status", result.Status, "item_id", result.ItemId)
	}
	return nil
}

// ItemPath resolves Jellyfin's opaque item ID to its library file path.
func (c *Client) ItemPath(ctx context.Context, id, userID string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("missing Jellyfin item ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Items/"+url.PathEscape(id), nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userID) != "" {
		query := req.URL.Query()
		query.Set("userId", userID)
		req.URL.RawQuery = query.Encode()
	}
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", c.apiKey))
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
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", c.apiKey))
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
