package model

import "time"

type MediaType string

const (
    MediaMovie  MediaType = "movie"
    MediaSeries MediaType = "series"
)

type State string

const (
    StateRequested State = "REQUESTED"
    StateAvailable State = "AVAILABLE"
    StateHot       State = "HOT"
    StateCooling   State = "COOLING"
    StateArchived  State = "ARCHIVED"
    StateBroken    State = "BROKEN"
    StateRearming  State = "REARMING"
    StatePruning   State = "PRUNING"
    StateFailed    State = "FAILED"
)

type MediaCacheState struct {
    ID              string
    Tenant          string
    MediaType       MediaType
    ArrID           int
    SymlinkPath     string
    State           State
    RearmRequested  bool
    CachedUntil     *time.Time
    TorBoxTorrentID *string
    RetryCount      int

    LastChecked    *time.Time
    LastRehydrated *time.Time
    LastPruned     *time.Time
    LastError      *string
}

type TorrentMetadata struct {
    InfoHash string
    Magnet   string
    Source   string
}

type TorBoxAddResult struct {
    TorrentID string
}
