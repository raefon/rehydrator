package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	ID          int            `json:"id"`
	MovieID     int            `json:"movieId"`
	SeriesID    int            `json:"seriesId"`
	SourceTitle string         `json:"sourceTitle"`
	DownloadID  string         `json:"downloadId"`
	EventType   string         `json:"eventType"`
	Data        map[string]any `json:"data"`
}

// LatestGrabbedTorrent returns the safest torrent metadata for rehydrating an Arr item.
// It validates movieId/seriesId and prefers the grabbed event that matches the latest import downloadId.
func (c *Client) LatestGrabbedTorrent(ctx context.Context, arrID int, mediaType model.MediaType) (model.TorrentMetadata, error) {
	records, err := c.history(ctx, arrID, mediaType)
	if err != nil {
		return model.TorrentMetadata{}, err
	}

	matching := make([]HistoryRecord, 0, len(records))
	for _, r := range records {
		if recordMatchesMedia(r, arrID, mediaType) {
			matching = append(matching, r)
		}
	}
	if len(matching) == 0 {
		return model.TorrentMetadata{}, fmt.Errorf("no matching %s history records found for arr_id=%d", mediaType, arrID)
	}

	if imported := latestImportRecord(matching); imported != nil {
		importDownloadID := firstNonEmpty(imported.DownloadID, dataString(imported.Data, "downloadId"))
		if importDownloadID != "" {
			for _, r := range matching {
				if r.EventType != "grabbed" || !sameDownloadID(r.DownloadID, importDownloadID) {
					continue
				}
				torrent, ok := torrentMetadataFromGrabbed(c.name, arrID, mediaType, r)
				if ok {
					logSelectedRecord(c.name, arrID, mediaType, r, "matched_import_download_id", torrent)
					return torrent, nil
				}
			}

			slog.Warn("found import event but no grabbed event with same download id",
				"arr", c.name,
				"media_type", mediaType,
				"arr_id", arrID,
				"import_history_id", imported.ID,
				"import_download_id", importDownloadID,
				"imported_path", dataString(imported.Data, "importedPath"),
				"dropped_path", dataString(imported.Data, "droppedPath"),
			)
		}
	}

	for _, r := range matching {
		if r.EventType != "grabbed" {
			continue
		}
		torrent, ok := torrentMetadataFromGrabbed(c.name, arrID, mediaType, r)
		if ok {
			logSelectedRecord(c.name, arrID, mediaType, r, "latest_matching_grabbed", torrent)
			return torrent, nil
		}
	}

	return model.TorrentMetadata{}, fmt.Errorf("no usable grabbed torrent found for %s arr_id=%d", mediaType, arrID)
}

func (c *Client) history(ctx context.Context, arrID int, mediaType model.MediaType) ([]HistoryRecord, error) {
	if c.base == "" || c.key == "" {
		return nil, fmt.Errorf("%s client is not configured", c.name)
	}

	endpoint := c.base + "/api/v3/history"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("pageSize", "250")
	q.Set("sortKey", "date")
	q.Set("sortDirection", "descending")

	switch mediaType {
	case model.MediaMovie:
		q.Set("movieId", fmt.Sprintf("%d", arrID))
	case model.MediaSeries:
		q.Set("seriesId", fmt.Sprintf("%d", arrID))
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.key)

	slog.Debug("arr history request", "arr", c.name, "url", req.URL.String(), "media_type", mediaType, "arr_id", arrID)

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
	if hr.Records == nil {
		return nil, errors.New("arr history response did not contain records")
	}

	return hr.Records, nil
}

func recordMatchesMedia(r HistoryRecord, arrID int, mediaType model.MediaType) bool {
	switch mediaType {
	case model.MediaMovie:
		return r.MovieID == arrID
	case model.MediaSeries:
		return r.SeriesID == arrID
	default:
		return false
	}
}

func latestImportRecord(records []HistoryRecord) *HistoryRecord {
	for i := range records {
		switch records[i].EventType {
		case "downloadFolderImported", "movieFileImported", "episodeFileImported":
			return &records[i]
		}
	}
	return nil
}

func torrentMetadataFromGrabbed(arrName string, arrID int, mediaType model.MediaType, r HistoryRecord) (model.TorrentMetadata, bool) {
	if r.EventType != "grabbed" {
		return model.TorrentMetadata{}, false
	}

	rawGuid := dataString(r.Data, "guid")
	rawDownloadURL := dataString(r.Data, "downloadUrl")

	infoHash := cleanInfoHash(firstNonEmpty(
		dataString(r.Data, "torrentInfoHash"),
		dataString(r.Data, "infoHash"),
		r.DownloadID,
		parseBTIH(rawGuid),
		parseBTIH(rawDownloadURL),
	))

	magnet := ""
	if infoHash != "" {
		magnet = buildMinimalMagnet(infoHash)
	} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawGuid)), "magnet:") {
		magnet = strings.TrimSpace(rawGuid)
	} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawDownloadURL)), "magnet:") {
		magnet = strings.TrimSpace(rawDownloadURL)
	}

	if infoHash == "" && magnet == "" {
		slog.Warn("grabbed arr history record has no usable torrent metadata",
			"arr", arrName,
			"media_type", mediaType,
			"arr_id", arrID,
			"history_id", r.ID,
			"movie_id", r.MovieID,
			"series_id", r.SeriesID,
			"source_title", r.SourceTitle,
			"download_id", r.DownloadID,
			"data_keys", dataKeys(r.Data),
		)
		return model.TorrentMetadata{}, false
	}

	return model.TorrentMetadata{
		InfoHash:    infoHash,
		Magnet:      magnet,
		Source:      arrName,
		SourceTitle: r.SourceTitle,
		DownloadID:  r.DownloadID,
	}, true
}

func logSelectedRecord(arrName string, arrID int, mediaType model.MediaType, r HistoryRecord, reason string, torrent model.TorrentMetadata) {
	slog.Info("selected arr grabbed history",
		"arr", arrName,
		"reason", reason,
		"media_type", mediaType,
		"arr_id", arrID,
		"history_id", r.ID,
		"movie_id", r.MovieID,
		"series_id", r.SeriesID,
		"source_title", r.SourceTitle,
		"download_id", r.DownloadID,
		"infohash", torrent.InfoHash,
		"magnet_len", len(torrent.Magnet),
		"magnet_prefix", firstN(torrent.Magnet, 30),
	)
}

func cleanInfoHash(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "urn:btih:")
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

func buildMinimalMagnet(infoHash string) string {
	infoHash = cleanInfoHash(infoHash)
	if infoHash == "" {
		return ""
	}
	return "magnet:?xt=urn:btih:" + infoHash
}

func parseBTIH(magnet string) string {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return ""
	}
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		lower := strings.ToLower(strings.TrimSpace(xt))
		if strings.HasPrefix(lower, "urn:btih:") {
			return cleanInfoHash(strings.TrimPrefix(lower, "urn:btih:"))
		}
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sameDownloadID(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	return a != "" && b != "" && a == b
}

func dataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func dataKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type Movie struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Path      string     `json:"path"`
	MovieFile *MovieFile `json:"movieFile"`
}

type MovieFile struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

func (m Movie) ImportedPath() string {
	if m.MovieFile == nil {
		return ""
	}
	return strings.TrimSpace(m.MovieFile.Path)
}

func (c *Client) Movies(ctx context.Context) ([]Movie, error) {
	if c.base == "" || c.key == "" {
		return nil, fmt.Errorf("%s client is not configured", c.name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v3/movie", nil)
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

	var movies []Movie
	if err := json.NewDecoder(resp.Body).Decode(&movies); err != nil {
		return nil, err
	}

	return movies, nil
}
