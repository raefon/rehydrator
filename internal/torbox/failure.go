package torbox

import "strings"

// FailureMessage recognizes explicit terminal provider states, not HTTP errors,
// stalled jobs, or arbitrary text containing "failed". Never echo raw state URLs.
func (d UsenetDownload) FailureMessage() (string, bool) {
	state := d.RawState
	if state == "" {
		state = d.State
	}
	state = strings.ToLower(strings.TrimSpace(state))
	failed := false
	for _, prefix := range []string{"failed", "aborted"} {
		if state == prefix {
			failed = true
			break
		}
		if strings.HasPrefix(state, prefix) {
			suffix := strings.TrimSpace(strings.TrimPrefix(state, prefix))
			if strings.HasPrefix(suffix, "(") || strings.HasPrefix(suffix, ":") || strings.HasPrefix(suffix, "-") {
				failed = true
				break
			}
		}
	}
	if !failed {
		return "", false
	}
	// Arr deliberately treats this SAB message as a warning, not a bad release.
	if strings.Contains(state, "disk full") || strings.Contains(state, "disk is full") || strings.Contains(state, "write error") {
		return "Unpacking failed, write error or disk is full?", true
	}
	if strings.Contains(state, "cannot be completed") || strings.Contains(state, "aborted") {
		return "TorBox aborted the download because it cannot be completed. Acquire a replacement release through Arr.", true
	}
	return "TorBox reported a terminal download failure. Acquire a replacement release through Arr.", true
}
