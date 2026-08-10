# TorBox, Decypharr, and CSI-rclone

## Decypharr

Re-arm/add uses Decypharr’s qBittorrent-compatible API:

```text
POST /api/v2/torrents/add
```

Rehydrator resolves infohash/magnet data from Radarr history and submits the torrent to Decypharr using the configured category.

## TorBox

Prune/delete uses TorBox by infohash. This keeps provider deletion authoritative even if CSI-rclone still shows a stale path.

## CSI-rclone visibility

CSI-rclone can be slow to reflect provider-side changes. Rehydrator treats this as a normal state:

```text
WAITING_FOR_VISIBILITY
```

Recommended defaults:

```yaml
csi:
  visibility_timeout_seconds: 900
  visibility_poll_seconds: 10
  visibility_retry_seconds: 60
```

## Provider cooldown

If Decypharr or TorBox API calls return rate-limit style errors, Rehydrator records a cooldown:

```yaml
provider_cooldown_seconds: 900
```

This does not directly detect WebDAV/rclone 429s unless those are surfaced to Rehydrator, but conservative worker/prune/re-arm limits reduce pressure on the provider.

## Conservative limits

```yaml
concurrent_workers: 2
max_rearms_per_run: 3
max_prunes_per_run: 5
reconcile_interval_seconds: 30
```

For large ingest, consider temporarily disabling prune:

```yaml
prune_enabled: false
```

Let Radarr/Decypharr/TorBox settle, then re-enable pruning.

## Regular `.torrent` files

v0.4.4 preserves non-magnet `downloadUrl` values from Radarr/Sonarr grabbed history. During a Rehydrator re-arm, the original torrent file is downloaded and submitted to Decypharr as multipart `torrents`, rather than being replaced with a minimal magnet.

This is important for private trackers because the original torrent can contain tracker-specific metadata that an infohash-only magnet does not reproduce.

This does **not** replace Decypharr's initial-download implementation. New `.torrent` grabs still travel directly from Radarr/Sonarr to Decypharr and require Decypharr's `/api/v2/torrents/add` implementation to correctly accept the qBittorrent multipart `torrents` upload and forward the file to the provider.
