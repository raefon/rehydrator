package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"net/http"
	"strings"
)

// RemoveMissingImportedFile invokes Arr's file-delete operation, not a
// database-only flag. Caller must confirm the exact file is missing beforehand.
func (c *Client) RemoveMissingImportedFile(ctx context.Context, item model.MediaCacheState, confirmMissing func(context.Context, string) (bool, error)) error {
	if !c.Configured() {
		return fmt.Errorf("Arr client is not configured")
	}
	endpoint := ""
	if item.MediaType == model.MediaSeries {
		if c.name != "sonarr" || item.SeriesID == nil {
			return fmt.Errorf("missing Sonarr series identity")
		}
		var file EpisodeFile
		found, err := c.missingFileRequest(ctx, http.MethodGet, fmt.Sprintf("/episodefile/%d", item.ArrID), &file)
		if err != nil || !found {
			return err
		}
		if file.ID != item.ArrID || file.SeriesID != *item.SeriesID || file.ImportedPath() != item.SymlinkPath {
			return fmt.Errorf("Sonarr file identity/path changed; refusing removal")
		}
		endpoint = fmt.Sprintf("/episodefile/%d", file.ID)
	} else if item.MediaType == model.MediaMovie {
		if c.name != "radarr" {
			return fmt.Errorf("wrong Arr client")
		}
		var movie Movie
		found, err := c.missingFileRequest(ctx, http.MethodGet, fmt.Sprintf("/movie/%d", item.ArrID), &movie)
		if err != nil || !found {
			return err
		}
		if movie.ID != item.ArrID {
			return fmt.Errorf("Radarr movie identity changed")
		}
		if movie.MovieFile == nil || movie.MovieFile.ID <= 0 {
			return nil
		}
		if movie.ImportedPath() != item.SymlinkPath {
			return fmt.Errorf("Radarr file path changed; refusing removal")
		}
		endpoint = fmt.Sprintf("/moviefile/%d", movie.MovieFile.ID)
	} else {
		return fmt.Errorf("unsupported media type")
	}
	if confirmMissing == nil {
		return fmt.Errorf("missing-file confirmation is required")
	}
	missing, err := confirmMissing(ctx, item.SymlinkPath)
	if err != nil {
		return err
	}
	if !missing {
		return fmt.Errorf("file is no longer confirmed missing; refusing removal")
	}
	_, err = c.missingFileRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if item.MediaType == model.MediaSeries {
		var file EpisodeFile
		found, err := c.missingFileRequest(ctx, http.MethodGet, endpoint, &file)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("Sonarr still reports the imported file after removal")
		}
	} else {
		var movie Movie
		found, err := c.missingFileRequest(ctx, http.MethodGet, fmt.Sprintf("/movie/%d", item.ArrID), &movie)
		if err != nil {
			return err
		}
		if found && movie.MovieFile != nil && movie.MovieFile.ID > 0 {
			return fmt.Errorf("Radarr still reports an imported file after removal")
		}
	}
	return nil
}
func (c *Client) missingFileRequest(ctx context.Context, method, endpoint string, result any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v3"+endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Api-Key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("%s file reconciliation %s: %s", c.name, strings.ToLower(method), resp.Status)
	}
	if result != nil {
		if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
			return false, err
		}
	}
	return true, nil
}
