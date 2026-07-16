package plex

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

type Client struct {
	baseURL        string
	token          string
	movieSectionID int
	tvSectionID    int
	httpClient     *http.Client
}

type Section struct {
	Key   int
	Title string
	Type  string
}

func NewClient(baseURL string, token string, movieSectionID int, tvSectionID int, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		token:          token,
		movieSectionID: movieSectionID,
		tvSectionID:    tvSectionID,
		httpClient:     &http.Client{Timeout: timeout},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

func (c *Client) RefreshMoviePath(ctx context.Context, mediaPath string) error {
	target := c.TargetScanPath(mediaPath, model.MediaMovie)
	return c.RefreshPathForMedia(ctx, target, model.MediaMovie)
}

func (c *Client) RefreshPath(ctx context.Context, plexPath string) error {
	return c.RefreshPathForMedia(ctx, plexPath, model.MediaMovie)
}

func (c *Client) RefreshPathForMedia(ctx context.Context, plexPath string, mediaType model.MediaType) error {
	if !c.Configured() {
		return fmt.Errorf("plex client is not configured")
	}
	sectionID, sectionType, err := c.sectionForMedia(ctx, mediaType)
	if err != nil {
		return err
	}
	values := url.Values{}
	values.Set("X-Plex-Token", c.token)
	if strings.TrimSpace(plexPath) != "" {
		values.Set("path", filepath.ToSlash(plexPath))
	}
	endpoint := fmt.Sprintf("%s/library/sections/%d/refresh?%s", c.baseURL, sectionID, values.Encode())
	_ = sectionType
	return c.doRefresh(ctx, endpoint)
}

func (c *Client) sectionForMedia(ctx context.Context, mediaType model.MediaType) (int, string, error) {
	switch mediaType {
	case model.MediaMovie:
		if c.movieSectionID > 0 {
			return c.movieSectionID, "movie", nil
		}
		id, err := c.FindFirstSection(ctx, "movie")
		return id, "movie", err
	case model.MediaSeries:
		if c.tvSectionID > 0 {
			return c.tvSectionID, "show", nil
		}
		id, err := c.FindFirstSection(ctx, "show")
		return id, "show", err
	default:
		return 0, "", fmt.Errorf("unsupported Plex media type: %s", mediaType)
	}
}

func (c *Client) TargetScanPath(mediaPath string, mediaType ...model.MediaType) string {
	mediaPath = strings.TrimSpace(mediaPath)
	if mediaPath == "" {
		return ""
	}
	clean := filepath.Clean(mediaPath)
	mt := model.MediaMovie
	if len(mediaType) > 0 {
		mt = mediaType[0]
	}
	if ext := filepath.Ext(clean); ext != "" {
		parent := filepath.Dir(clean)
		if mt == model.MediaSeries {
			// For episode files, scan the season folder instead of the whole show
			// when paths use the normal /Show/Season xx/file layout.
			return filepath.ToSlash(parent)
		}
		return filepath.ToSlash(parent)
	}
	return filepath.ToSlash(clean)
}

func (c *Client) RefreshMovies(ctx context.Context) error {
	if !c.Configured() {
		return fmt.Errorf("plex client is not configured")
	}
	sectionID := c.movieSectionID
	if sectionID <= 0 {
		discovered, err := c.FindFirstSection(ctx, "movie")
		if err != nil {
			return err
		}
		sectionID = discovered
	}
	values := url.Values{}
	values.Set("X-Plex-Token", c.token)
	endpoint := fmt.Sprintf("%s/library/sections/%d/refresh?%s", c.baseURL, sectionID, values.Encode())
	return c.doRefresh(ctx, endpoint)
}

func (c *Client) RefreshTV(ctx context.Context) error {
	if !c.Configured() {
		return fmt.Errorf("plex client is not configured")
	}
	sectionID := c.tvSectionID
	if sectionID <= 0 {
		discovered, err := c.FindFirstSection(ctx, "show")
		if err != nil {
			return err
		}
		sectionID = discovered
	}
	values := url.Values{}
	values.Set("X-Plex-Token", c.token)
	endpoint := fmt.Sprintf("%s/library/sections/%d/refresh?%s", c.baseURL, sectionID, values.Encode())
	return c.doRefresh(ctx, endpoint)
}

func (c *Client) doRefresh(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("plex refresh failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.FindFirstSection(ctx, "movie")
	if err != nil {
		return err
	}
	if c.tvSectionID > 0 {
		_, err = c.FindFirstSection(ctx, "show")
	}
	return err
}

func (c *Client) FindFirstMovieSection(ctx context.Context) (int, error) {
	return c.FindFirstSection(ctx, "movie")
}

func (c *Client) FindFirstSection(ctx context.Context, sectionType string) (int, error) {
	if !c.Configured() {
		return 0, fmt.Errorf("plex client is not configured")
	}
	values := url.Values{}
	values.Set("X-Plex-Token", c.token)
	endpoint := c.baseURL + "/library/sections?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("plex sections request failed: %s", resp.Status)
	}
	var parsed struct {
		Directories []struct {
			Key   string `xml:"key,attr"`
			Title string `xml:"title,attr"`
			Type  string `xml:"type,attr"`
		} `xml:"Directory"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, err
	}
	for _, d := range parsed.Directories {
		if strings.EqualFold(d.Type, sectionType) {
			id, err := strconv.Atoi(d.Key)
			if err == nil && id > 0 {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("no Plex %s section found; configure Plex section id", sectionType)
}
