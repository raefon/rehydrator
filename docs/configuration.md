# Configuration

This file explains the main `rehydrator.yaml` sections. Start with `config/rehydrator.example.yaml`.

## Core

```yaml
postgres_url: "postgres://<USER>:<PASSWORD>@<POSTGRES_HOST>:5432/<DATABASE>?sslmode=disable"
tenant: tenet-nofear101
health_addr: ":8080"
db_auto_migrate: true
csi_path: /storage/media
```

`tenant` is stored on every row and should match the namespace/customer you are running.

## Reconcile and safety limits

```yaml
reconcile_interval_seconds: 30
cache_grace_hours: 24
max_retries: 10
concurrent_workers: 2
prune_enabled: true
rearm_enabled: true
max_prunes_per_run: 5
max_rearms_per_run: 3
provider_cooldown_seconds: 900
```

Keep these conservative for TorBox/WebDAV. Large bursts can cause rate limits or slow CSI-rclone visibility.

## CSI visibility

```yaml
csi_wait_seconds: 900
csi:
  visibility_timeout_seconds: 900
  visibility_poll_seconds: 10
  visibility_retry_seconds: 60
```

If Decypharr add succeeds but the path is not visible yet, Rehydrator should use `WAITING_FOR_VISIBILITY` and retry visibility checks instead of re-adding the torrent.

## Radarr

```yaml
radarr:
  url: http://tenet-radarr.tenet-nofear101.svc.cluster.local:7878
  api_key: "<RADARR_API_KEY>"

radarr_sync:
  enabled: true
  interval_seconds: 60
```

Radarr is the source of imported movie paths, TMDb IDs, and download history used to resolve infohash/magnet metadata.

## Decypharr

```yaml
decypharr:
  url: http://tenet-decypharr.tenet-nofear101.svc.cluster.local:8282
  username: ""
  password: ""
  radarr_category: radarr
  sonarr_category: sonarr
  delete_files_on_prune: true
```

Re-arm/add goes through Decypharr’s qBittorrent-compatible API.

## TorBox

```yaml
torbox:
  api_key: "<TORBOX_API_KEY>"
```

Prune/delete goes directly to TorBox by infohash. Provider delete is authoritative; CSI-rclone can remain stale after provider-side delete.

## Seerr

```yaml
seerr:
  url: http://tenet-seerr.tenet-nofear101.svc.cluster.local:5055
  api_key: "<SEERR_API_KEY>"
  sync:
    enabled: true
    interval_seconds: 300
    limit: 100
```

Seerr provides request/watchlist intent and can create placeholder rows before Radarr import completes.

## Playback

```yaml
playback:
  enabled: true
  rearm_on_play: true
  cooldown_seconds: 60
  ignored_titles:
    - rehydrator-preroll
  ignored_title_contains:
    - preroll
    - pre-roll
```

Plex pre-rolls send normal playback webhooks. Ignore pre-roll titles to avoid unmatched playback noise and unnecessary Radarr refreshes.

## Plex hygiene

```yaml
plex:
  enabled: true
  url: http://tenet-plex.tenet-nofear101.svc.cluster.local:32400
  token: "<PLEX_TOKEN>"
  movie_section_id: 0
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_after_prune: false
  refresh_delay_seconds: 45
  refresh_timeout_seconds: 20
  max_refreshes_per_run: 5
```

Refresh after re-arm/visibility helps clear Plex unavailable/trash indicators. Keep `refresh_after_prune` disabled by default.

## rclone RC

```yaml
rclone_rc:
  enabled: false
  url: http://localhost:5572
  username: ""
  password: ""
  refresh_after_rearm: false
  timeout_seconds: 10
```

Only enable this if your CSI-rclone deployment exposes rclone RC safely.

## Secrets

Do not commit live values for:

```text
postgres_url
radarr.api_key
torbox.api_key
seerr.api_key
api.token
plex.token
```

## Self-heal worker

```yaml
self_heal:
  enabled: true
  interval_seconds: 300
  plex_refresh_available: true
  plex_recent_hours: 24
  max_plex_refreshes_per_run: 5
```

The self-heal worker covers the v0.3.3 behavior inside the v0.3.2 package. It periodically queues targeted Plex refreshes for recently rehydrated or recently played `AVAILABLE` movies that have not had a successful Plex refresh since that event.

It does not scan the whole library, and it does not refresh after prune.
