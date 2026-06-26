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
)

type Client struct {
	baseURL        string
	token          string
	movieSectionID int
	httpClient     *http.Client
}

type Section struct {
	Key   int
	Title string
	Type  string
}

func NewClient(baseURL string, token string, movieSectionID int, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		token:          token,
		movieSectionID: movieSectionID,
		httpClient:     &http.Client{Timeout: timeout},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

func (c *Client) RefreshMoviePath(ctx context.Context, mediaPath string) error {
	if !c.Configured() {
		return fmt.Errorf("plex client is not configured")
	}
	sectionID := c.movieSectionID
	if sectionID <= 0 {
		discovered, err := c.FindFirstMovieSection(ctx)
		if err != nil {
			return err
		}
		sectionID = discovered
	}
	values := url.Values{}
	values.Set("X-Plex-Token", c.token)
	if strings.TrimSpace(mediaPath) != "" {
		values.Set("path", filepath.ToSlash(mediaPath))
	}
	endpoint := fmt.Sprintf("%s/library/sections/%d/refresh?%s", c.baseURL, sectionID, values.Encode())
	return c.doRefresh(ctx, endpoint)
}

func (c *Client) RefreshMovies(ctx context.Context) error {
	if !c.Configured() {
		return fmt.Errorf("plex client is not configured")
	}
	sectionID := c.movieSectionID
	if sectionID <= 0 {
		discovered, err := c.FindFirstMovieSection(ctx)
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

func (c *Client) FindFirstMovieSection(ctx context.Context) (int, error) {
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
		if strings.EqualFold(d.Type, "movie") {
			id, err := strconv.Atoi(d.Key)
			if err == nil && id > 0 {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("no Plex movie section found; configure plex.movie_section_id")
}
