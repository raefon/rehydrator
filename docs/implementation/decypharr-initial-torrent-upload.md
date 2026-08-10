# Decypharr initial `.torrent` upload contract

Rehydrator v0.4.4 sends torrent files to Decypharr using the qBittorrent-compatible API shape:

```text
POST /api/v2/torrents/add
Content-Type: multipart/form-data

torrents=<actual .torrent bytes>
paused=false
category=<category>
```

Decypharr should forward the actual torrent bytes to the TorBox torrent-file endpoint as:

```text
POST /v1/api/torrents/createtorrent
Content-Type: multipart/form-data

file=<actual .torrent bytes>
```

Do not replace the file with an infohash-only magnet. This would lose private-tracker metadata.

The existing magnet path should remain unchanged.
