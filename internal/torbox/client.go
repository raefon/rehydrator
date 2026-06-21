package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

	"github.com/raefon/rehydrator/internal/httpx"
	"github.com/raefon/rehydrator/internal/model"
)

type Client struct {
	key  string
	base string
	http *http.Client
}

func NewClient(key string) *Client {
	return &Client{
		key:  key,
		base: "https://api.torbox.app/v1/api",
		http: httpx.DefaultClient(),
	}
}

type addResponse struct {
	Data any `json:"data"`
}

func (c *Client) AddTorrent(ctx context.Context, torrent model.TorrentMetadata) (model.TorBoxAddResult, error) {
	if torrent.Magnet == "" {
		return model.TorBoxAddResult{}, fmt.Errorf("missing magnet; TorBox createtorrent does not accept bare infohash")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("magnet", torrent.Magnet); err != nil {
		return model.TorBoxAddResult{}, err
	}

	// Optional but useful. Keeps behavior closer to "instant cached only"
	// if your TorBox API/account supports it.
	_ = writer.WriteField("add_only_if_cached", "false")

	if err := writer.Close(); err != nil {
		return model.TorBoxAddResult{}, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.base+"/torrents/createtorrent",
		&body,
	)
	if err != nil {
		return model.TorBoxAddResult{}, err
	}

	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	slog.Info("torbox create torrent request", "url", req.URL.String())

	resp, err := c.http.Do(req)
	if err != nil {
		return model.TorBoxAddResult{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return model.TorBoxAddResult{}, fmt.Errorf("unexpected status: %s body=%s", resp.Status, string(raw))
	}

	var decoded addResponse
	_ = json.Unmarshal(raw, &decoded)

	return model.TorBoxAddResult{TorrentID: extractID(decoded.Data)}, nil
}

func (c *Client) DeleteTorrent(ctx context.Context, torrentID string) error {
	if torrentID == "" {
		return fmt.Errorf("missing torbox torrent id")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		c.base+"/torrents/"+torrentID,
		nil,
	)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return httpx.CheckStatus(resp)
}

func extractID(data any) string {
	switch v := data.(type) {
	case map[string]any:
		for _, key := range []string{"id", "torrent_id", "torrentId"} {
			if raw, ok := v[key]; ok {
				return fmt.Sprint(raw)
			}
		}
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}
