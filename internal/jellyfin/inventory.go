package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// InventoryPaths returns only a complete, consistently paginated snapshot.
// Missing paths must never be inferred from a failed or partial response.
func (c *Client) InventoryPaths(ctx context.Context) (map[string]bool, error) {
	paths := map[string]bool{}
	seen := map[string]bool{}
	total := -1
	for offset := 0; offset < 100000; {
		q := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode"}, "Fields": {"Path,MediaSources"}, "EnableTotalRecordCount": {"true"}, "EnableImages": {"false"}, "EnableUserData": {"false"}, "SortBy": {"SortName"}, "StartIndex": {strconv.Itoa(offset)}, "Limit": {"500"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/Items?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", c.apiKey))
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items []struct {
				ID           string
				Path         string
				MediaSources []struct{ Path string }
			}
			TotalRecordCount *int
			StartIndex       *int
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("Jellyfin inventory HTTP %d", resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("Jellyfin inventory: %w", err)
		}
		if page.TotalRecordCount == nil || *page.TotalRecordCount < 0 || page.Items == nil || page.StartIndex == nil || *page.StartIndex != offset {
			return nil, fmt.Errorf("incomplete Jellyfin inventory page")
		}
		if total < 0 {
			total = *page.TotalRecordCount
		}
		if total != *page.TotalRecordCount {
			return nil, fmt.Errorf("Jellyfin inventory changed during pagination")
		}
		for _, item := range page.Items {
			if item.ID == "" || seen[item.ID] {
				return nil, fmt.Errorf("Jellyfin inventory pagination did not advance")
			}
			seen[item.ID] = true
			if p := InventoryPath(item.Path); p != "" {
				paths[p] = true
			}
			for _, source := range item.MediaSources {
				if p := InventoryPath(source.Path); p != "" {
					paths[p] = true
				}
			}
		}
		offset += len(page.Items)
		if offset == total {
			return paths, nil
		}
		if len(page.Items) == 0 || offset > total {
			return nil, fmt.Errorf("incomplete Jellyfin inventory")
		}
	}
	return nil, fmt.Errorf("Jellyfin inventory exceeded safety limit")
}

func InventoryPath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	return path.Clean(p)
}

// MapInventoryPath uses the longest directory-prefix match, never substrings.
func MapInventoryPath(p string, mappings map[string]string) string {
	p = InventoryPath(p)
	if p == "" {
		return ""
	}
	best := ""
	target := ""
	for from, to := range mappings {
		from = InventoryPath(from)
		to = InventoryPath(to)
		if from == "" || to == "" {
			continue
		}
		if (p == from || strings.HasPrefix(p, strings.TrimRight(from, "/")+"/")) && len(from) > len(best) {
			best = from
			target = to
		}
	}
	if best != "" {
		return path.Join(target, strings.TrimPrefix(p, best))
	}
	return p
}
