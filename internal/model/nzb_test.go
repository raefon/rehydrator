package model

import "testing"

const validNZB = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="episode.mkv"><groups><group>alt.binaries.example</group></groups><segments><segment bytes="1024" number="1">article@example</segment></segments></file></nzb>`

func TestNZBSource(t *testing.T) {
	base := TorrentMetadata{Kind: SourceNZBFile, Protocol: ProtocolUsenet, NZBFile: []byte(validNZB), DownloadID: "opaque-job-id"}
	source, err := base.ValidateSource()
	if err != nil || source.InfoHash != "" || source.NZBFilename != "download.nzb" || source.DownloadID != "opaque-job-id" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	for _, data := range []string{"", "<html>oops</html>", "<nzb/>", "<nzb><file>", "<nzb><segment>invalid-location</segment></nzb>"} {
		bad := base
		bad.NZBFile = []byte(data)
		if _, err := bad.ValidateSource(); err == nil {
			t.Fatalf("invalid NZB accepted: %s", data)
		}
	}
	for _, kind := range []string{"hash", "magnet", "torrent", "protocol"} {
		bad := base
		switch kind {
		case "hash":
			bad.InfoHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "magnet":
			bad.Magnet = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "torrent":
			bad.TorrentFile = []byte("torrent")
		case "protocol":
			bad.Protocol = ProtocolTorrent
		}
		if _, err := bad.ValidateSource(); err == nil {
			t.Fatalf("mixed %s accepted", kind)
		}
	}
	if base.NZBFingerprint() != source.NZBFingerprint() {
		t.Fatal("source fingerprint changed")
	}
}

func TestDownloadProtocol(t *testing.T) {
	for _, tc := range []struct{ protocol, client, want string }{{"1", "", ProtocolUsenet}, {"usenet", "", ProtocolUsenet}, {"", "Sabnzbd", ProtocolUsenet}, {"2", "", ProtocolTorrent}, {"", "qBittorrent", ""}, {"", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""}} {
		if got := DownloadProtocol(tc.protocol, tc.client); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}
