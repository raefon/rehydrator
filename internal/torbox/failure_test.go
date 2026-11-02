package torbox

import (
	"strings"
	"testing"
)

func TestTerminalProviderFailureStates(t *testing.T) {
	for _, state := range []string{"failed", "FAILED (Processing)", "failed (Aborted, cannot be completed - https://sabnzbd.org/not-complete)", "aborted", "failed: repair failed", "failed (https://private.example/?apikey=secret)"} {
		d := UsenetDownload{State: state}
		message, failed := d.FailureMessage()
		if !failed || message == "" || strings.Contains(message, "https://") || strings.Contains(message, "secret") {
			t.Fatal(state, message, failed)
		}
		if observed := d.Status(1, true); observed.State != "failed" || observed.Ready {
			t.Fatal("failure details leaked or readiness incorrect", observed)
		}
	}
	for _, state := range []string{"downloading", "stalled (no seeds)", "Direct Unpack: Completed", "processing", "repairing (failed blocks)", "paused", "failedness", "failed to connect"} {
		if message, failed := (UsenetDownload{State: state}).FailureMessage(); failed {
			t.Fatal(state, message)
		}
	}
	d := UsenetDownload{State: "cached", RawState: "failed (Processing)", Files: []UsenetFile{{Name: "file.mkv", Size: 10}}}
	if d.Ready() {
		t.Fatal("normalized ready state overrode terminal provider failure")
	}
	message, failed := (UsenetDownload{State: "failed (Unpacking failed, disk is full)"}).FailureMessage()
	if !failed || message != "Unpacking failed, write error or disk is full?" {
		t.Fatal(message, failed)
	}
}
