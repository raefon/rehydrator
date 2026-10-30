package seerr

import "testing"

func TestTVRequestParsing(t *testing.T) {
	req, ok := DecodeRequest([]byte(`{"id":42,"status":2,"media":{"mediaType":"tv","tmdbId":100,"tvdbId":200},"seasons":[{"seasonNumber":1},{"seasonNumber":3}]}`))
	if !ok || req.TVDBID != 200 || req.TMDBID != 100 || len(req.Seasons) != 2 || req.Seasons[1] != 3 || !Approved(req.Status) {
		t.Fatalf("%+v %v", req, ok)
	}
	pending, ok := DecodeRequest([]byte(`{"id":43,"status":1,"media":{"mediaType":"tv","tvdbId":200},"seasons":[2]}`))
	if !ok || Approved(pending.Status) {
		t.Fatal("pending request approved")
	}
	if len(Seasons(nil)) != 0 || len(Seasons("all")) != 0 {
		t.Fatal("missing scope widened")
	}
}
