package model

import "testing"

func TestTorrentFileIdentity(t *testing.T) {
	for _, data := range []string{"d4:infod4:name7:exampleee", "d8:announce3:url4:infod4:name7:exampleee"} {
		hash, err := TorrentFileInfoHash([]byte(data))
		if err != nil || hash != "7952549975b3256302ada821a1295c5a1ca628dc" {
			t.Fatalf("hash=%s err=%v", hash, err)
		}
	}
	for _, data := range []string{"", "<html>", "de", "d4:info3:abce", "d4:infod4:name999:xe", "d4:infod4:name7:examplee", "d4:infod4:name7:exampleeejunk", "d4:infode4:infodee"} {
		if _, err := TorrentFileInfoHash([]byte(data)); err == nil {
			t.Errorf("invalid torrent accepted: %q", data)
		}
	}
}
