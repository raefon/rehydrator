package torbox

import (
	"math"
	"time"
)

// ProviderStatus is a whitelisted observation; it contains no source URLs or keys.
type ProviderStatus struct {
	ProviderID    int64     `json:"providerId"`
	CheckedAt     time.Time `json:"checkedAt"`
	LastAttemptAt time.Time `json:"lastAttemptAt"`
	CheckFailed   bool      `json:"checkFailed"`
	Found         bool      `json:"found"`
	State         string    `json:"state"`
	Progress      *float64  `json:"progress"`
	Speed         *float64  `json:"speed"`
	Size          *float64  `json:"size"`
	Downloaded    *float64  `json:"downloaded"`
	Finished      *bool     `json:"finished"`
	Present       *bool     `json:"present"`
	Cached        *bool     `json:"cached"`
	Ready         bool      `json:"ready"`
}

func validNumber(value *float64, max float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || (max > 0 && *value > max) {
		return nil
	}
	return value
}
func (d UsenetDownload) Status(providerID int64, found bool) ProviderStatus {
	now := time.Now().UTC()
	s := ProviderStatus{ProviderID: providerID, CheckedAt: now, LastAttemptAt: now, Found: found}
	if !found {
		return s
	}
	state := d.RawState
	if state == "" {
		state = d.State
	}
	s.State = state
	s.Progress = validNumber(d.Progress, 1)
	s.Speed = validNumber(d.Speed, 0)
	s.Size = validNumber(d.Size, 0)
	s.Downloaded = validNumber(d.Downloaded, 0)
	s.Finished = d.Finished
	s.Present = d.Present
	s.Cached = d.Cached
	s.Ready = d.Ready()
	return s
}
