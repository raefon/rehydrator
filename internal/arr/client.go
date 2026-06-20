package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/raefon/rehydrator/internal/httpx"
	"github.com/raefon/rehydrator/internal/model"
)

type Client struct {
	name string
	base string
	key  string
	http *http.Client
}

func NewClient(name, base, key string) *Client {
	return &Client{
		name: strings.TrimSpace(name),
		base: strings.TrimRight(base, "/"),
		key:  key,
		http: httpx.DefaultClient(),
	}
}

type HistoryResponse struct {
	Page         int             `json:"page"`
	PageSize     int             `json:"pageSize"`
	TotalRecords int             `json:"totalRecords"`
	Records      []HistoryRecord `json:"records"`
}

type HistoryRecord struct {
	ID          int               `json:"id"`
	MovieID     int               `json:"movieId"`
	SeriesID    int               `json:"seriesId"`
	SourceTitle string            `json:"sourceTitle"`
	DownloadID  string            `json:"downloadId"`
	EventType   string            `json:"eventType"`
	Data        map[string]string `json:"data"`
}

func (c *Client) LatestGrabbedTorrent(ctx context.Context, arrID int, mediaType model.MediaType) (model.TorrentMetadata, error) {
	records, err := c.history(ctx, arrID, mediaType)
	if err != nil {
		return model.TorrentMetadata{}, err
	}

	for _, r := range records {
		if r.EventType != "grabbed" {
			continue
		}

		infoHash := firstNonEmpty(r.Data["torrentInfoHash"], r.DownloadID)
		magnet := firstNonEmpty(r.Data["guid"], r.Data["downloadUrl"])

		if strings.HasPrefix(strings.ToLower(magnet), "magnet:") && infoHash == "" {
			infoHash = parseBTIH(magnet)
		}

		if infoHash == "" && magnet == "" {
			continue
		}

		return model.TorrentMetadata{
			InfoHash: strings.ToLower(infoHash),
			Magnet:   magnet,
			Source:   c.name,
		}, nil
	}

	return model.TorrentMetadata{}, errors.New("no grabbed torrent history record found")
}

func (c *Client) history(ctx context.Context, arrID int, mediaType model.MediaType) ([]HistoryRecord, error) {
	if c.base == "" || c.key == "" {
		return nil, fmt.Errorf("%s client is not configured", c.name)
	}

	endpoint := c.base + "/api/v3/history"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "100")
	q.Set("sortKey", "date")
	q.Set("sortDirection", "descending")

	if mediaType == model.MediaMovie {
		q.Set("movieId", fmt.Sprintf("%d", arrID))
	} else {
		q.Set("seriesId", fmt.Sprintf("%d", arrID))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
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

	var hr HistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return nil, err
	}

	return hr.Records, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseBTIH(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		lower := strings.ToLower(xt)
		if strings.HasPrefix(lower, "urn:btih:") {
			return strings.TrimPrefix(lower, "urn:btih:")
		}
	}
	return ""
}
