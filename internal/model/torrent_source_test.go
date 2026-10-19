package model

import (
	"errors"
	"testing"
)

func TestExclusiveTorrentSource(t *testing.T) {
	const hash = "7952549975b3256302ada821a1295c5a1ca628dc"
	const magnet = "magnet:?xt=urn:btih:" + hash
	const file = "d4:infod4:name7:exampleee"
	for _, tc := range []struct {
		name   string
		source TorrentMetadata
		valid  bool
	}{
		{"file", TorrentMetadata{Kind: SourceTorrentFile, TorrentFile: []byte(file)}, true},
		{"magnet", TorrentMetadata{Kind: SourceMagnet, Magnet: magnet}, true},
		{"hash_only", TorrentMetadata{InfoHash: hash}, false},
		{"missing_kind", TorrentMetadata{Magnet: magnet}, false},
		{"file_and_magnet", TorrentMetadata{Kind: SourceTorrentFile, TorrentFile: []byte(file), Magnet: magnet}, false},
		{"magnet_and_file", TorrentMetadata{Kind: SourceMagnet, Magnet: magnet, TorrentFile: []byte(file)}, false},
		{"wrong_kind", TorrentMetadata{Kind: SourceTorrentFile, Magnet: magnet}, false},
		{"unresolved_url", TorrentMetadata{Kind: SourceTorrentFile, TorrentURL: "http://indexer/file"}, false},
		{"file_hash_conflict", TorrentMetadata{Kind: SourceTorrentFile, TorrentFile: []byte(file), InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, false},
		{"magnet_hash_conflict", TorrentMetadata{Kind: SourceMagnet, Magnet: magnet, InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := tc.source.ValidateSource()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && source.InfoHash != hash {
				t.Fatal("incorrect release identity")
			}
		})
	}
	if _, _, err := ResolveMagnet("", hash); !errors.Is(err, ErrSourceRecoveryRequired) {
		t.Fatal("hash was converted to a magnet")
	}
}
