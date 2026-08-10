# Torrent-file re-arm support

## Why this exists

Some indexers, especially private trackers, provide a `.torrent` file rather than a magnet. Rehydrator previously reduced Arr history to an infohash/minimal magnet, which is not equivalent to forwarding the original torrent metadata.

## v0.4.4 behavior

When Radarr/Sonarr history contains a non-magnet `downloadUrl`, Rehydrator stores it in the in-memory torrent metadata for the re-arm operation. When re-arming, the Decypharr client:

1. Fetches the torrent file from the Arr/Prowlarr-provided URL.
2. Limits the download to 50 MiB.
3. Sends the actual bytes to `POST /api/v2/torrents/add` as the qBittorrent-compatible multipart `torrents` field.
4. Keeps the original infohash for lifecycle/state tracking.

If no usable torrent-file URL exists, the existing magnet/infohash path is used.

## What is preserved

The original torrent bytes are forwarded. Rehydrator does not reconstruct a torrent from the infohash, so private-tracker metadata is retained.

## Initial downloads vs. re-arms

This release only changes the Rehydrator-controlled **re-arm** path. Initial grabs still happen directly from Radarr/Sonarr to Decypharr. Decypharr must therefore support receiving a multipart `.torrent` upload itself for initial IPT/private-tracker downloads.

## Logs

A torrent-file re-arm includes:

```text
submission_mode=torrent_file
torrent_url_present=true
```

The torrent URL itself is never logged, because it may contain an indexer/Prowlarr API key.
