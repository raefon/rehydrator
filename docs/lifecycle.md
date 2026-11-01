# Download lifecycle

1. Arr sends a magnet/torrent through qBittorrent API or an NZB through SAB API.
2. The gateway saves the original source and a stable job before acknowledging it.
3. The bounded worker submits to TorBox and polls provider state.
4. Ready provider files appear in the shared read-only WebDAV catalog.
5. The controller stages links and verifies mounted-file readability.
6. Only then does the gateway report Completed for Arr to import.
7. Import/playback/inventory hooks update library state and request Jellyfin refreshes.

The gateway owns provider-loss recovery. Three missing observations and an exact
retained source are required; unknown submissions never authorize a blind upload.
Attempts and submission spacing remain bounded. Torrent and NZB identities stay
separate. Library rearm requests use that same gateway source/catalog rather than
re-fetching an old Prowlarr URL.

No automatic provider pruning is performed. The local cached-until timestamp is
informational; it does not delete a TorBox placement.
