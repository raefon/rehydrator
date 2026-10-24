package controller

import (
	"github.com/raefon/rehydrator/internal/decypharr"
	"testing"
)

func TestProviderRetryPreservesActiveAndUnknownJobs(t *testing.T) {
	if !providerRetryQueueIdle(nil) {
		t.Fatal("absent job should permit retry")
	}
	for _, state := range []string{"downloading", "stalledDL", "queuedDL", "pausedDL", "checkingDL", "allocating", "metaDL", "", "future-state"} {
		if providerRetryQueueIdle(&decypharr.TorrentInfo{State: state}) {
			t.Fatalf("unsafe retry for %q", state)
		}
	}
	for _, state := range []string{"uploading", "stalledUP", "pausedUP", "stoppedUP", "queuedUP", "error", "missingFiles"} {
		if !providerRetryQueueIdle(&decypharr.TorrentInfo{State: state}) {
			t.Fatalf("retry blocked for %q", state)
		}
	}
}
