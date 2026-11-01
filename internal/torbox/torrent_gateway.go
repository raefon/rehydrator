package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

var ErrSubmissionNotStarted = errors.New("provider submission not started")

type TorrentClient struct{ *UsenetClient }

func NewTorrentClient(key string) *TorrentClient { return &TorrentClient{NewUsenetClient(key)} }
func (c *TorrentClient) Submit(ctx context.Context, source model.TorrentMetadata) (int64, error) {
	s, err := source.ValidateSource()
	if err != nil {
		return 0, err
	}
	if s.Kind == model.SourceTorrentFile {
		if err = model.ValidateGatewayTorrent(s.TorrentFile); err != nil {
			return 0, err
		}
	}
	health := &HealthClient{key: c.key, base: c.base, http: c.http}
	observation, e := health.LookupTorrent(ctx, s.InfoHash)
	if e != nil {
		return 0, fmt.Errorf("%w: %v", ErrSubmissionNotStarted, e)
	}
	if observation.TorrentID > 0 {
		if observation.Status == Available || observation.Status == Unknown {
			return observation.TorrentID, nil
		}
		return 0, fmt.Errorf("%w: stale torrent placement still exists in TorBox; inspect provider before readding", ErrSubmissionNotStarted)
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if s.Kind == model.SourceMagnet {
		err = w.WriteField("magnet", s.Magnet)
	} else if s.Kind == model.SourceTorrentFile {
		var f interface{ Write([]byte) (int, error) }
		f, err = w.CreateFormFile("file", s.TorrentFilename)
		if err == nil {
			_, err = f.Write(s.TorrentFile)
		}
	} else {
		return 0, fmt.Errorf("torrent source required")
	}
	if err != nil {
		return 0, err
	}
	_ = w.WriteField("seed", "1")
	_ = w.WriteField("allow_zip", "false")
	_ = w.WriteField("as_queued", "false")
	if err = w.Close(); err != nil {
		return 0, err
	}
	raw, err := c.envelope(ctx, http.MethodPost, "/torrents/createtorrent", w.FormDataContentType(), &b)
	if err != nil {
		return 0, err
	}
	var result struct {
		ID       int64 `json:"torrent_id"`
		QueuedID int64 `json:"queued_id"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return 0, fmt.Errorf("TorBox returned an invalid torrent submission response")
	}
	if result.ID <= 0 {
		if result.QueuedID > 0 {
			return 0, fmt.Errorf("TorBox accepted torrent into provider queue (queued_id=%d), not an active torrent; start the existing queued item and reconcile its active ID by infohash", result.QueuedID)
		}
		return 0, fmt.Errorf("TorBox did not return a torrent ID")
	}
	return result.ID, nil
}
func (c *TorrentClient) Get(ctx context.Context, id int64) (UsenetDownload, bool, error) {
	raw, err := c.envelope(ctx, "GET", "/torrents/mylist?id="+strconv.FormatInt(id, 10)+"&bypass_cache=true", "", nil)
	if err != nil {
		return UsenetDownload{}, false, err
	}
	var entries []UsenetDownload
	if json.Unmarshal(raw, &entries) != nil {
		var one UsenetDownload
		if json.Unmarshal(raw, &one) != nil || one.ID <= 0 {
			return one, false, fmt.Errorf("invalid TorBox torrent inventory")
		}
		entries = []UsenetDownload{one}
	}
	for _, d := range entries {
		if d.ID != id {
			return d, false, fmt.Errorf("unexpected TorBox torrent identity")
		}
		if err = ValidateUsenetFiles(d.Files); err != nil {
			return d, false, err
		}
		d.RawState = d.State
		if d.Present != nil && *d.Present && len(d.Files) > 0 {
			d.State = "cached"
		} else if d.Present == nil {
			return d, false, fmt.Errorf("TorBox torrent readiness is unknown")
		}
		return d, true, nil
	}
	return UsenetDownload{}, false, nil
}
func (c *TorrentClient) Link(ctx context.Context, id, file int64) (string, error) {
	q := url.Values{"token": {c.key}, "torrent_id": {strconv.FormatInt(id, 10)}, "file_id": {strconv.FormatInt(file, 10)}, "redirect": {"false"}}
	raw, err := c.envelope(ctx, "GET", "/torrents/requestdl?"+q.Encode(), "", nil)
	if err != nil {
		return "", err
	}
	var link string
	if json.Unmarshal(raw, &link) != nil {
		return "", fmt.Errorf("invalid torrent link")
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid delivery URL")
	}
	return link, nil
}

// FindActive only adopts an exact positive hash/ID match in the account listing.
// Queue identities never become placement IDs; incomplete listings cannot prove
// absence or permit a new submission.
func (c *TorrentClient) FindActive(ctx context.Context, hash string) (int64, error) {
	health := &HealthClient{key: c.key, base: c.base, http: c.http}
	observation, err := health.LookupTorrent(ctx, hash)
	if err != nil {
		return 0, err
	}
	if observation.Status == Available || observation.Status == Unknown {
		return observation.TorrentID, nil
	}
	return 0, nil
}
