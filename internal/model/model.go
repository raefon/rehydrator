package model

import "time"

type MediaType string

const (
	MediaMovie MediaType = "movie"
	// MediaSeries is the series/episode-file lifecycle type. v0.4.1 stores one
	// row per imported Sonarr episode file and uses ArrID as episode_file_id.
	MediaSeries MediaType = "series"
)

type State string

const (
	StateRequested         State = "REQUESTED"
	StateAvailable         State = "AVAILABLE"
	StateHot               State = "HOT"
	StateCooling           State = "COOLING"
	StateArchived          State = "ARCHIVED"
	StateBroken            State = "BROKEN"
	StateRearming          State = "REARMING"
	StateWaitingVisibility State = "WAITING_FOR_VISIBILITY"
	StatePruning           State = "PRUNING"
	StateFailed            State = "FAILED"
)

type MediaCacheState struct {
	SourceDownloadID string
	SourceProtocol   string
	DownloadJobID    string
	ID               string
	Tenant           string
	MediaType        MediaType
	ArrID            int // movie: Radarr movie id; series: Sonarr episodeFileId.
	SymlinkPath      string
	State            State

	RearmRequested bool
	CachedUntil    *time.Time

	// Provider-specific legacy/debug field. Current prune flow looks this up by InfoHash.
	TorBoxTorrentID *string

	// Stored release identity and selected download backend.
	InfoHash         *string
	Magnet           *string
	DownloadClient   *string
	DownloadCategory *string
	ArrTitle         *string
	SourceTitle      *string
	TMDBID           *int
	TVDBID           *int

	// v0.4.1 Sonarr episode-file metadata. Movies leave these nil/empty.
	SeriesID      *int
	EpisodeID     *int
	EpisodeFileID *int
	SeasonNumber  *int
	EpisodeNumber *int
	SeriesTitle   *string
	EpisodeTitle  *string
	CacheGroupKey *string

	RetryCount  int
	NextRetryAt *time.Time

	LastPlayIntentAt *time.Time
	PlayIntentCount  int

	LastChecked    *time.Time
	LastRehydrated *time.Time
	LastPruned     *time.Time
	LastError      *string
}

// TorrentMetadata is retained as the compatibility name for a download source.
type TorrentMetadata struct {
	Protocol        string
	NZBFile         []byte
	NZBFilename     string
	Kind            SourceKind
	TorrentFile     []byte
	TorrentFilename string
	InfoHash        string
	Magnet          string
	TorrentURL      string
	Source          string
	SourceTitle     string
	DownloadID      string
}

type DownloadClientAddResult struct {
	Hash string
}

// TorBoxAddResult is kept for compatibility with the legacy TorBox client package.
type TorBoxAddResult struct {
	TorrentID string
}
