package model

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type SourceKind string

const (
	SourceNZBFile          SourceKind = "nzb_file"
	SourceTorrentFile      SourceKind = "torrent_file"
	SourceMagnet           SourceKind = "magnet"
	SourceUnresolved       SourceKind = "unresolved"
	SourceRecoveryRequired SourceKind = "recovery_required"
)

var ErrSourceRecoveryRequired = errors.New("source recovery required: original torrent file, magnet, or NZB is missing")

func NormalizeInfoHash(hash string) (string, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	_, err := hex.DecodeString(hash)
	if len(hash) != 40 || err != nil {
		return "", fmt.Errorf("expected a 40-character v1 infohash")
	}
	return hash, nil
}

// ValidateSource enforces a single saved submission source. Hashes are identity,
// never an instruction to manufacture a magnet or reconstruct metainfo.
func (t TorrentMetadata) ValidateSource() (TorrentMetadata, error) {
	if t.Kind == SourceNZBFile {
		return t.validateNZB()
	}
	if (t.Protocol != "" && t.Protocol != ProtocolTorrent) || len(t.NZBFile) > 0 || t.NZBFilename != "" {
		return t, fmt.Errorf("torrent source conflicts with Usenet payload")
	}
	t.Protocol = "torrent"
	if strings.TrimSpace(t.TorrentURL) != "" {
		return t, fmt.Errorf("unresolved URL is not a saved submission source")
	}
	var hash string
	var err error
	switch t.Kind {
	case SourceTorrentFile:
		if len(t.TorrentFile) == 0 || t.Magnet != "" {
			return t, fmt.Errorf("torrent_file requires bytes and forbids a magnet")
		}
		if len(t.TorrentFile) > 50<<20 {
			return t, fmt.Errorf("torrent file exceeds size limit")
		}
		hash, err = TorrentFileInfoHash(t.TorrentFile)
		if err == nil && t.InfoHash != "" && !strings.EqualFold(strings.TrimSpace(t.InfoHash), hash) {
			err = fmt.Errorf("torrent file does not match stored infohash")
		}
		if t.TorrentFilename == "" {
			t.TorrentFilename = "download.torrent"
		}
	case SourceMagnet:
		if len(t.TorrentFile) > 0 || t.TorrentFilename != "" {
			return t, fmt.Errorf("magnet forbids torrent bytes and filename")
		}
		t.TorrentFile = nil
		t.Magnet, hash, err = ResolveMagnet(t.Magnet, t.InfoHash)
	case SourceUnresolved, SourceRecoveryRequired, "":
		return t, ErrSourceRecoveryRequired
	default:
		return t, fmt.Errorf("unsupported torrent source kind: %s", t.Kind)
	}
	if err != nil {
		return t, err
	}
	t.InfoHash = hash
	return t, nil
}

// ResolveMagnet preserves trackers and rejects conflicting release identities.
func ResolveMagnet(magnet, hash string) (string, string, error) {
	magnet = strings.TrimSpace(magnet)
	hash = strings.ToLower(strings.TrimSpace(hash))
	validHash := func(s string) bool { _, err := hex.DecodeString(s); return len(s) == 40 && err == nil }
	if hash != "" && !validHash(hash) {
		return "", "", fmt.Errorf("expected a 40-character v1 infohash")
	}
	if magnet != "" {
		u, err := url.Parse(magnet)
		if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
			return "", "", fmt.Errorf("invalid magnet URI")
		}
		magnetHash := ""
		for _, xt := range u.Query()["xt"] {
			if strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
				magnetHash = strings.ToLower(xt[len("urn:btih:"):])
				break
			}
		}
		if !validHash(magnetHash) {
			return "", "", fmt.Errorf("magnet requires a 40-character v1 infohash")
		}
		if hash != "" && hash != magnetHash {
			return "", "", fmt.Errorf("magnet and stored infohash disagree")
		}
		return magnet, magnetHash, nil
	}
	if hash == "" {
		return "", "", fmt.Errorf("missing magnet/infohash")
	}
	return "", "", ErrSourceRecoveryRequired
}
