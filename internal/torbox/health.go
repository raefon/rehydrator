package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raefon/rehydrator/internal/httpx"
	"github.com/raefon/rehydrator/internal/model"
)

type Observation struct {
	Status    Availability
	Reason    string
	TorrentID int64
}
type Availability string

const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
	Unknown     Availability = "unknown"
)

// HealthClient exposes only read-only provider observations. No mutation methods.
type HealthClient struct {
	key, base string
	http      *http.Client
}

func NewHealthClient(key string) *HealthClient {
	return &HealthClient{key: strings.TrimSpace(key), base: defaultBaseURL, http: httpx.DefaultClient()}
}

// Availability checks account download_present, not merely global cache presence.
// A successful complete account listing is required before inferring absence.
func (c *HealthClient) Availability(ctx context.Context, hashes []string) (map[string]Observation, error) {
	if c.key == "" {
		return nil, fmt.Errorf("TorBox health API key is missing")
	}
	states := map[string]Observation{}
	for _, input := range hashes {
		hash, err := model.NormalizeInfoHash(input)
		if err != nil {
			return nil, err
		}
		states[hash] = Observation{Status: Unavailable, Reason: "not_in_account"}
	}
	if len(states) == 0 {
		return states, nil
	}
	seen := map[int64]bool{}
	offset := 0
	for page := 0; page < 100; page++ {
		if page > 0 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		q := url.Values{"bypass_cache": {"true"}, "limit": {"1000"}, "offset": {strconv.Itoa(offset)}}
		req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/torrents/mylist?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("TorBox health request failed")
		}
		var result struct {
			Success bool `json:"success"`
			Data    *[]struct {
				ID      int64  `json:"id"`
				Hash    string `json:"hash"`
				Present *bool  `json:"download_present"`
				State   string `json:"download_state"`
			}
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("TorBox health returned HTTP %d", resp.StatusCode)
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result)
		resp.Body.Close()
		if err != nil || !result.Success || result.Data == nil {
			return nil, fmt.Errorf("invalid/incomplete TorBox health response")
		}
		newIDs := 0
		for _, entry := range *result.Data {
			if entry.ID <= 0 {
				return nil, fmt.Errorf("TorBox health response missing torrent identity")
			}
			if !seen[entry.ID] {
				newIDs++
				seen[entry.ID] = true
			}
			hash := strings.ToLower(strings.TrimSpace(entry.Hash))
			if _, err := model.NormalizeInfoHash(hash); err != nil {
				return nil, fmt.Errorf("TorBox health response has invalid hash")
			}
			if _, wanted := states[hash]; !wanted {
				continue
			}
			active := false
			switch strings.ToLower(entry.State) {
			case "downloading", "metadl", "checkingresumedata", "queueddl", "stalleddl", "allocating":
				active = true
			}
			if entry.Present == nil || (active && !*entry.Present) {
				if states[hash].Status != Available {
					states[hash] = Observation{Status: Unknown, Reason: "incomplete_or_active_download", TorrentID: entry.ID}
				}
			} else if *entry.Present {
				states[hash] = Observation{Status: Available, Reason: "download_present", TorrentID: entry.ID}
			} else if states[hash].Status != Available && states[hash].Status != Unknown {
				states[hash] = Observation{Status: Unavailable, Reason: "download_not_present", TorrentID: entry.ID}
			}
		}
		if len(*result.Data) == 0 {
			return states, nil
		}
		offset += len(*result.Data)
		if newIDs == 0 {
			return nil, fmt.Errorf("TorBox health pagination did not advance")
		}
	}
	return nil, fmt.Errorf("TorBox health listing exceeded page limit")
}
