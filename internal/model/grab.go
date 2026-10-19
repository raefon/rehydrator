package model

// GrabSource survives queue cleanup and exists before a movie/episode is imported.
// OwnerID is the Radarr movie ID or Sonarr series ID, never an episode-file ID.
type GrabSource struct {
	Tenant     string
	Arr        string
	OwnerID    int
	DownloadID string
	Torrent    TorrentMetadata
}
