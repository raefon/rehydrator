package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type UsenetFile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type UsenetDownload struct {
	RawState   string       `json:"-"`
	Progress   *float64     `json:"progress"`
	Speed      *float64     `json:"download_speed"`
	Size       *float64     `json:"size"`
	Downloaded *float64     `json:"total_downloaded"`
	Finished   *bool        `json:"download_finished"`
	Cached     *bool        `json:"cached"`
	ID         int64        `json:"id"`
	Name       string       `json:"name"`
	State      string       `json:"download_state"`
	Present    *bool        `json:"download_present"`
	Files      []UsenetFile `json:"files"`
}

func (d UsenetDownload) Ready() bool {
	state := strings.ToLower(d.State)
	return (state == "cached" || state == "completed") && (d.Present == nil || *d.Present) && len(d.Files) > 0
}
func ValidateUsenetFiles(files []UsenetFile) error {
	seen := map[string]bool{}
	for _, f := range files {
		if f.ID < 0 || f.Size < 0 || f.Name == "" || strings.HasPrefix(f.Name, "/") || strings.ContainsAny(f.Name, "\\\x00") || path.Clean(f.Name) != f.Name || f.Name == ".." || strings.HasPrefix(f.Name, "../") || seen[f.Name] {
			return fmt.Errorf("invalid cloud file catalog")
		}
		seen[f.Name] = true
	}
	return nil
}

type UsenetClient struct {
	key, base string
	http      *http.Client
}

func NewUsenetClient(key string) *UsenetClient {
	return &UsenetClient{key: key, base: defaultBaseURL, http: &http.Client{Timeout: 45 * time.Second}}
}
func (c *UsenetClient) envelope(ctx context.Context, method, endpoint, contentType string, body io.Reader) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("invalid TorBox request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TorBox Usenet request failed or timed out")
	}
	defer resp.Body.Close()
	var result struct {
		Success bool
		Error   string
		Data    json.RawMessage
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid TorBox Usenet response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success {
		return nil, fmt.Errorf("TorBox Usenet HTTP %d: %s", resp.StatusCode, result.Error)
	}
	if len(result.Data) == 0 || string(result.Data) == "null" {
		return nil, fmt.Errorf("TorBox Usenet response missing data")
	}
	return result.Data, nil
}
func (c *UsenetClient) Create(ctx context.Context, nzb []byte, filename string) (int64, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", filename)
	if err != nil {
		return 0, err
	}
	if _, err = f.Write(nzb); err != nil {
		return 0, err
	}
	_ = w.WriteField("post_processing", "-1")
	_ = w.WriteField("as_queued", "false")
	if err = w.Close(); err != nil {
		return 0, err
	}
	raw, err := c.envelope(ctx, http.MethodPost, "/usenet/createusenetdownload", w.FormDataContentType(), &body)
	if err != nil {
		return 0, err
	}
	var result struct {
		ID       int64 `json:"usenetdownload_id"`
		QueuedID int64 `json:"queued_id"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return 0, fmt.Errorf("TorBox returned an invalid Usenet submission response")
	}
	if result.ID <= 0 {
		if result.QueuedID > 0 {
			return 0, fmt.Errorf("TorBox accepted NZB into provider queue (queued_id=%d), not an active Usenet download; inspect the existing provider job before retrying", result.QueuedID)
		}
		return 0, fmt.Errorf("TorBox did not return a Usenet download ID")
	}

	return result.ID, nil
}
func (c *UsenetClient) Get(ctx context.Context, id int64) (UsenetDownload, bool, error) {
	raw, err := c.envelope(ctx, http.MethodGet, "/usenet/mylist?id="+strconv.FormatInt(id, 10)+"&bypass_cache=true", "", nil)
	if err != nil {
		return UsenetDownload{}, false, err
	}
	var entries []UsenetDownload
	if json.Unmarshal(raw, &entries) != nil {
		var one UsenetDownload
		if json.Unmarshal(raw, &one) != nil || one.ID <= 0 {
			return one, false, fmt.Errorf("invalid TorBox Usenet inventory")
		}
		entries = []UsenetDownload{one}
	}
	for _, entry := range entries {
		if entry.ID != id {
			return UsenetDownload{}, false, fmt.Errorf("TorBox returned unexpected Usenet identity")
		}
		if err = ValidateUsenetFiles(entry.Files); err != nil {
			return entry, false, err
		}
		return entry, true, nil
	}
	return UsenetDownload{}, false, nil
}
func (c *UsenetClient) Link(ctx context.Context, id, file int64) (string, error) {
	q := url.Values{"token": {c.key}, "usenet_id": {strconv.FormatInt(id, 10)}, "file_id": {strconv.FormatInt(file, 10)}, "redirect": {"false"}}
	raw, err := c.envelope(ctx, http.MethodGet, "/usenet/requestdl?"+q.Encode(), "", nil)
	if err != nil {
		return "", err
	}
	var link string
	if json.Unmarshal(raw, &link) != nil {
		return "", fmt.Errorf("invalid TorBox Usenet link")
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid TorBox delivery URL")
	}
	return link, nil
}
