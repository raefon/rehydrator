package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const ProtocolUsenet = "usenet"
const ProtocolTorrent = "torrent"

// DownloadProtocol uses explicit Arr protocol/client metadata, never ID shape.
func DownloadProtocol(protocol, clientType string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "usenet", "nzb", "1":
		return ProtocolUsenet
	case "torrent", "2":
		return ProtocolTorrent
	}
	switch strings.ToLower(clientType) {
	case "sabnzbd", "nzbget":
		return ProtocolUsenet
	}
	return ""
}

func (t TorrentMetadata) validateNZB() (TorrentMetadata, error) {
	if t.Protocol != "" && t.Protocol != ProtocolUsenet {
		return t, fmt.Errorf("NZB protocol conflict")
	}
	if len(t.NZBFile) == 0 || len(t.NZBFile) > 50<<20 || len(t.TorrentFile) > 0 || t.TorrentFilename != "" || t.Magnet != "" || t.InfoHash != "" || t.TorrentURL != "" {
		return t, fmt.Errorf("nzb_file requires only original NZB bytes, not a torrent or hash")
	}
	d := xml.NewDecoder(bytes.NewReader(t.NZBFile))
	var root bool
	var stack []string
	segments := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return t, fmt.Errorf("invalid NZB XML")
		}
		switch v := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if root || v.Name.Local != "nzb" {
					return t, fmt.Errorf("expected NZB root")
				}
				root = true
			}
			stack = append(stack, v.Name.Local)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 4 && stack[0] == "nzb" && stack[1] == "file" && stack[2] == "segments" && stack[3] == "segment" && strings.TrimSpace(string(v)) != "" {
				segments++
			}
		}
	}
	if !root || segments == 0 {
		return t, fmt.Errorf("NZB contains no article segments")
	}
	t.Protocol = ProtocolUsenet
	if t.NZBFilename == "" {
		t.NZBFilename = "download.nzb"
	}
	return t, nil
}

// Fingerprint identifies preserved bytes; it is not a torrent infohash or job ID.
func (t TorrentMetadata) NZBFingerprint() string {
	sum := sha256.Sum256(t.NZBFile)
	return hex.EncodeToString(sum[:])
}

// DownloadKey canonicalizes torrent hashes while preserving opaque job IDs.
func DownloadKey(id string) string {
	id = strings.TrimSpace(id)
	if hash, err := NormalizeInfoHash(id); err == nil {
		return hash
	}
	return id
}
