package torbox

import (
	"encoding/json"
	"math"
	"testing"
)

func TestProviderTelemetryOptionalValues(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    *float64
	}{
		{`{"progress":0}`, number(0)}, {`{"progress":0.5}`, number(.5)}, {`{"progress":1}`, number(1)}, {`{}`, nil}, {`{"progress":null}`, nil}, {`{"progress":50}`, nil}, {`{"progress":-1}`, nil},
	} {
		var d UsenetDownload
		if err := json.Unmarshal([]byte(tc.payload), &d); err != nil {
			t.Fatal(err)
		}
		got := d.Status(1, true).Progress
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Fatal(tc.payload, got)
		}
	}
	d := UsenetDownload{Progress: number(math.NaN()), Speed: number(-1)}
	if s := d.Status(1, true); s.Progress != nil || s.Speed != nil {
		t.Fatal("invalid metrics published")
	}
}
func number(value float64) *float64 { return &value }
func TestDownloadCompleteDoesNotMeanReady(t *testing.T) {
	var d UsenetDownload
	if err := json.Unmarshal([]byte(`{"download_state":"Direct Unpack: Completed","download_present":false,"download_finished":true,"progress":1,"download_speed":0,"size":1024}`), &d); err != nil {
		t.Fatal(err)
	}
	s := d.Status(7, true)
	if s.Ready || s.State != "Direct Unpack: Completed" || s.Progress == nil || *s.Progress != 1 || s.Finished == nil || !*s.Finished {
		t.Fatal(s)
	}
	d.RawState = "uploading"
	d.State = "cached"
	s = d.Status(7, true)
	if s.State != "uploading" {
		t.Fatal("raw provider state replaced")
	}
	s = d.Status(7, false)
	if s.Progress != nil || s.State != "" || s.Found || s.Ready {
		t.Fatal("missing placement retained obsolete metrics")
	}
}
