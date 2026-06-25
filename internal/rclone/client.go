package rclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	mountRoot  string
	httpClient *http.Client
}

func NewClient(baseURL string, username string, password string, mountRoot string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		mountRoot:  filepath.Clean(mountRoot),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) RefreshForPath(ctx context.Context, mediaPath string) error {
	if !c.Configured() || strings.TrimSpace(mediaPath) == "" {
		return nil
	}
	dir := filepath.Dir(mediaPath)
	if c.mountRoot != "" && c.mountRoot != "." {
		cleanRoot := filepath.Clean(c.mountRoot)
		cleanDir := filepath.Clean(dir)
		if rel, err := filepath.Rel(cleanRoot, cleanDir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			dir = rel
		} else if rel == "." {
			dir = ""
		}
	}
	dir = filepath.ToSlash(strings.TrimPrefix(dir, "/"))
	payload, _ := json.Marshal(map[string]any{
		"dir":       dir,
		"recursive": false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/vfs/refresh", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rclone rc vfs/refresh failed: %s", resp.Status)
	}
	return nil
}
