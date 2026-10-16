package decypharr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strings"

	"github.com/raefon/rehydrator/internal/httpx"
	"github.com/raefon/rehydrator/internal/model"
)

type Client struct {
	base     string
	username string
	password string
	http     *http.Client
}

type TorrentInfo struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Category    string  `json:"category"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
	Progress    float64 `json:"progress"`
}

func NewClient(base, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	httpClient := httpx.DefaultClient()
	httpClient.Jar = jar

	return &Client{
		base:     strings.TrimRight(strings.TrimSpace(base), "/"),
		username: strings.TrimSpace(username),
		password: password,
		http:     httpClient,
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.base != ""
}

func (c *Client) Ping(ctx context.Context) error {
	if !c.Configured() {
		return fmt.Errorf("decypharr client is not configured")
	}
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v2/app/version", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return httpx.CheckStatus(resp)
}

func (c *Client) AddTorrent(ctx context.Context, torrent model.TorrentMetadata, category string) (model.DownloadClientAddResult, error) {
	if c.base == "" {
		return model.DownloadClientAddResult{}, fmt.Errorf("missing Decypharr URL")
	}

	if err := c.authenticate(ctx); err != nil {
		return model.DownloadClientAddResult{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	submissionMode := "magnet"

	torrentURL := strings.TrimSpace(torrent.TorrentURL)
	magnet := strings.TrimSpace(torrent.Magnet)

	slog.Info("decypharr torrent source diagnostic",
		"torrent_url_scheme", func() string {
			if i := strings.Index(torrentURL, ":"); i >= 0 {
				return strings.ToLower(torrentURL[:i])
			}
			return ""
		}(),
		"torrent_url_is_magnet", strings.HasPrefix(strings.ToLower(torrentURL), "magnet:"),
		"magnet_present", magnet != "",
		"infohash_present", strings.TrimSpace(torrent.InfoHash) != "",
	)

	// Some Arr history entries put the magnet URI in TorrentURL rather
	// than Magnet. Normalize that case before choosing the submission mode.
	if strings.HasPrefix(strings.ToLower(torrentURL), "magnet:") {
		magnet = torrentURL
		torrentURL = ""
	}

	if torrentURL != "" {
		torrentBytes, filename, err := c.fetchTorrentFile(ctx, torrentURL, torrent.SourceTitle)
		if err != nil {
			return model.DownloadClientAddResult{}, fmt.Errorf("fetch torrent file: %w", err)
		}

		part, err := writer.CreateFormFile("torrents", filename)
		if err != nil {
			return model.DownloadClientAddResult{}, err
		}
		if _, err := part.Write(torrentBytes); err != nil {
			return model.DownloadClientAddResult{}, err
		}
		submissionMode = "torrent_file"
	} else {
		if magnet == "" && torrent.InfoHash != "" {
			magnet = "magnet:?xt=urn:btih:" + strings.ToLower(strings.TrimSpace(torrent.InfoHash))
		}
		if magnet == "" {
			return model.DownloadClientAddResult{}, fmt.Errorf("missing magnet/infohash for Decypharr add")
		}
		if err := writer.WriteField("urls", magnet); err != nil {
			return model.DownloadClientAddResult{}, err
		}
	}

	if err := writer.WriteField("paused", "false"); err != nil {
		return model.DownloadClientAddResult{}, err
	}
	if category != "" {
		if err := writer.WriteField("category", category); err != nil {
			return model.DownloadClientAddResult{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return model.DownloadClientAddResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/torrents/add", &body)
	if err != nil {
		return model.DownloadClientAddResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	slog.Info("decypharr add torrent request",
		"url", req.URL.String(),
		"category", category,
		"infohash", torrent.InfoHash,
		"magnet_len", len(torrent.Magnet),
		"torrent_url_present", strings.TrimSpace(torrent.TorrentURL) != "",
		"submission_mode", submissionMode,
	)

	raw, status, err := c.do(req)
	if err != nil {
		return model.DownloadClientAddResult{}, err
	}
	if status < 200 || status > 299 {
		return model.DownloadClientAddResult{}, fmt.Errorf("decypharr add unexpected status: %d body=%s", status, string(raw))
	}

	hash := strings.ToLower(strings.TrimSpace(torrent.InfoHash))
	return model.DownloadClientAddResult{Hash: hash}, nil
}

const maxTorrentFileSize = 50 << 20

func (c *Client) fetchTorrentFile(ctx context.Context, torrentURL, sourceTitle string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if err := httpx.CheckStatus(resp); err != nil {
		return nil, "", fmt.Errorf("download URL returned %w", err)
	}
	if resp.ContentLength > maxTorrentFileSize {
		return nil, "", fmt.Errorf("torrent file is too large: %d bytes", resp.ContentLength)
	}

	limited := io.LimitReader(resp.Body, maxTorrentFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxTorrentFileSize {
		return nil, "", fmt.Errorf("torrent file exceeds %d-byte limit", maxTorrentFileSize)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("torrent file is empty")
	}

	filename := "download.torrent"
	if u, err := url.Parse(torrentURL); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" && base != "" {
			filename = base
		}
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".torrent") {
		filename += ".torrent"
	}
	if strings.TrimSpace(sourceTitle) != "" && filename == "download.torrent" {
		filename = strings.TrimSpace(sourceTitle) + ".torrent"
	}
	return data, filename, nil
}

func (c *Client) DeleteTorrent(ctx context.Context, infoHash string, deleteFiles bool) error {
	infoHash = strings.ToLower(strings.TrimSpace(infoHash))
	if infoHash == "" {
		return fmt.Errorf("missing infohash for Decypharr delete")
	}
	if c.base == "" {
		return fmt.Errorf("missing Decypharr URL")
	}

	if err := c.authenticate(ctx); err != nil {
		return err
	}

	form := url.Values{}
	form.Set("hashes", infoHash)
	form.Set("deleteFiles", fmt.Sprintf("%t", deleteFiles))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/torrents/delete", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	slog.Info("decypharr delete torrent request",
		"url", req.URL.String(),
		"infohash", infoHash,
		"delete_files", deleteFiles,
	)

	raw, status, err := c.do(req)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("decypharr delete unexpected status: %d body=%s", status, string(raw))
	}

	return nil
}

func (c *Client) TorrentInfo(ctx context.Context, infoHash string) (*TorrentInfo, error) {
	infoHash = strings.ToLower(strings.TrimSpace(infoHash))
	if infoHash == "" {
		return nil, fmt.Errorf("missing infohash for Decypharr info")
	}
	if c.base == "" {
		return nil, fmt.Errorf("missing Decypharr URL")
	}

	if err := c.authenticate(ctx); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("hashes", infoHash)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v2/torrents/info?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	raw, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("decypharr info unexpected status: %d body=%s", status, string(raw))
	}

	var infos []TorrentInfo
	if err := json.Unmarshal(raw, &infos); err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, nil
	}

	return &infos[0], nil
}

func (c *Client) authenticate(ctx context.Context) error {
	if c.username == "" && c.password == "" {
		return nil
	}

	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	raw, status, err := c.do(req)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("decypharr auth unexpected status: %d body=%s", status, string(raw))
	}

	body := strings.TrimSpace(string(raw))
	if body != "" && !strings.EqualFold(body, "Ok.") {
		return fmt.Errorf("decypharr auth failed: %s", body)
	}

	return nil
}

func (c *Client) do(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}

	return raw, resp.StatusCode, nil
}
