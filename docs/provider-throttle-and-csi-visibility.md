# Provider throttle and CSI-rclone visibility tuning

This Rehydrator version is conservative by default because TorBox/WebDAV and CSI-rclone can lag or throttle under heavy Plex/Radarr reads.

## Symptoms

A common slow-mount case looks like this:

```text
Decypharr add succeeds
TorBox shows torrent cached
/storage/media path is still missing for 5+ minutes
old behavior: CSI path did not appear within 3m0s
```

v0.2.9 changes that behavior to `WAITING_FOR_VISIBILITY`. The item is not failed and Rehydrator does not re-add the torrent. It only retries CSI path checks until the file appears.

## Recommended Rehydrator settings

```yaml
concurrent_workers: 2
max_rearms_per_run: 3
max_prunes_per_run: 5
reconcile_interval_seconds: 30

csi:
  visibility_timeout_seconds: 900
  visibility_poll_seconds: 10
  visibility_retry_seconds: 60

provider_cooldown_seconds: 900
```

For a large initial import, disable prune until Radarr/Decypharr/TorBox settle:

```yaml
prune_enabled: false
rearm_enabled: true
```

Then turn prune back on once the library is seeded and quiet.

## Optional rclone RC refresh

If your CSI-rclone deployment exposes rclone RC, Rehydrator can call `vfs/refresh` after a successful Decypharr re-add.

```yaml
rclone_rc:
  enabled: true
  url: http://csi-rclone-rc:5572
  username: ""
  password: ""
  refresh_after_rearm: true
  timeout_seconds: 10
```

This is optional. If the CSI driver does not expose rclone RC, leave it disabled.

## Suggested rclone mount posture

For TorBox/WebDAV, prefer fewer concurrent reads over max throughput:

```text
--dir-cache-time=5m
--poll-interval=0
--vfs-cache-mode=full
--vfs-read-chunk-size=8M
--vfs-read-chunk-size-limit=64M
--vfs-read-chunk-streams=1
--transfers=1
--checkers=2
--tpslimit=2
--tpslimit-burst=4
```

Avoid recursive `find`, `du`, Plex video preview thumbnails, chapter thumbnails, intro/credits analysis, and loudness analysis against the remote mount during heavy ingest.

## Plex settings

Keep these disabled for provider-friendly operation:

```text
Generate video preview thumbnails: Never
Generate chapter thumbnails: Never
Intro detection: Off or scheduled only
Credits detection: Off or scheduled only
Audio loudness analysis: Off or scheduled only
Extensive media analysis: Off
Scan automatically: Off
Partial scan on changes: Off
Empty trash automatically: Off
```
