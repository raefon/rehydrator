package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		base: "https://api.torbox.app/v1",
		http: httpx.DefaultClient(),
	}
}

type addResponse struct {
	Data any `json:"data"`
}

func (c *Client) AddTorrent(ctx context.Context, torrent model.TorrentMetadata) (model.TorBoxAddResult, error) {
	if torrent.InfoHash == "" && torrent.Magnet == "" {
		return model.TorBoxAddResult{}, fmt.Errorf("missing infohash and magnet")
	}

	body := map[string]string{}
	if torrent.InfoHash != "" {
		body["hash"] = torrent.InfoHash
	}
	if torrent.Magnet != "" {
		body["magnet"] = torrent.Magnet
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return model.TorBoxAddResult{}, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.base+"/torrents/createtorrent",
		bytes.NewReader(raw),
	)
	if err != nil {
		return model.TorBoxAddResult{}, err
	}

	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return model.TorBoxAddResult{}, err
	}
	defer resp.Body.Close()

	if err := httpx.CheckStatus(resp); err != nil {
		return model.TorBoxAddResult{}, err
	}

	var decoded addResponse
	_ = json.NewDecoder(resp.Body).Decode(&decoded)

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
