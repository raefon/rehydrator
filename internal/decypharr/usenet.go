package decypharr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

func (c *Client) prepareNZB(ctx context.Context, t model.TorrentMetadata) (model.TorrentMetadata, error) {
	if t.Magnet != "" || len(t.TorrentFile) > 0 || t.InfoHash != "" {
		return t, fmt.Errorf("NZB cannot contain torrent sources")
	}
	if t.TorrentURL != "" {
		if len(t.NZBFile) > 0 {
			return t, fmt.Errorf("NZB URL conflicts with saved bytes")
		}
		u, err := url.Parse(t.TorrentURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return t, fmt.Errorf("NZB requires HTTP download URL")
		}
		data, name, err := c.fetchSourceFile(ctx, t.TorrentURL, t.SourceTitle, ".nzb")
		if err != nil {
			return t, fmt.Errorf("fetch NZB file: %w", err)
		}
		t.NZBFile = data
		t.NZBFilename = name
		t.TorrentURL = ""
	}
	t.Kind = model.SourceNZBFile
	t.Protocol = model.ProtocolUsenet
	return t.ValidateSource()
}

func (c *Client) sabFields(mode, category string) url.Values {
	v := url.Values{"mode": {mode}, "output": {"json"}, "cat": {category}}
	if c.apiToken != "" {
		v.Set("apikey", c.apiToken)
	} else {
		v.Set("ma_username", c.username)
		v.Set("ma_password", c.password)
	}
	return v
}

// AddNZB uploads original NZB bytes and returns the opaque SAB job ID.
func (c *Client) AddNZB(ctx context.Context, t model.TorrentMetadata, category string) (string, error) {
	t, err := t.ValidateSource()
	if err != nil || t.Kind != model.SourceNZBFile {
		return "", fmt.Errorf("invalid NZB source")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, vs := range c.sabFields("addfile", category) {
		if err := w.WriteField(k, vs[0]); err != nil {
			return "", err
		}
	}
	part, err := w.CreateFormFile("name", t.NZBFilename)
	if err != nil {
		return "", err
	}
	if _, err = part.Write(t.NZBFile); err != nil {
		return "", err
	}
	if err = w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/sabnzbd/api", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	raw, status, err := c.do(req)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("Decypharr NZB add returned HTTP %d", status)
	}
	var result struct {
		Status bool
		IDs    []string `json:"nzo_ids"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("invalid SAB add response")
	}
	if !result.Status || len(result.IDs) != 1 || strings.TrimSpace(result.IDs[0]) == "" {
		return "", fmt.Errorf("SAB did not accept one NZB job")
	}
	return result.IDs[0], nil
}

// NZBStatus never treats a missing/failed job or a successful HTTP response as completion.
func (c *Client) NZBStatus(ctx context.Context, id, category string) (ready, found bool, err error) {
	if strings.TrimSpace(id) == "" {
		return false, false, fmt.Errorf("missing NZB job ID")
	}
	for _, mode := range []string{"history", "queue"} {
		v := c.sabFields(mode, category)
		v.Set("nzo_ids", id)
		req, e := http.NewRequestWithContext(ctx, "POST", c.base+"/sabnzbd/api", strings.NewReader(v.Encode()))
		if e != nil {
			return false, false, e
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		raw, status, e := c.do(req)
		if e != nil {
			return false, false, e
		}
		if status != 200 {
			return false, false, fmt.Errorf("SAB status returned HTTP %d", status)
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(raw, &result) != nil {
			return false, false, fmt.Errorf("invalid SAB status response")
		}
		if rawStatus, ok := result["status"]; ok {
			var accepted bool
			if json.Unmarshal(rawStatus, &accepted) != nil || !accepted {
				return false, false, fmt.Errorf("SAB status request rejected")
			}
		}
		var container struct {
			Slots []struct {
				ID     string `json:"nzo_id"`
				Status string
			}
		}
		data, ok := result[mode]
		if !ok || json.Unmarshal(data, &container) != nil || container.Slots == nil {
			return false, false, fmt.Errorf("SAB status missing %s slots", mode)
		}
		for _, slot := range container.Slots {
			if slot.ID == id {
				if strings.EqualFold(slot.Status, "Failed") {
					return false, true, fmt.Errorf("%w: Decypharr NZB job failed; recover source or select another release through Arr", model.ErrSourceRecoveryRequired)
				}
				return mode == "history" && strings.EqualFold(slot.Status, "Completed"), true, nil
			}
		}
	}
	return false, false, nil
}
