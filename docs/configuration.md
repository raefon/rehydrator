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

After provider add, Rehydrator uses `WAITING_FOR_VISIBILITY` to retry provider readiness and visibility checks instead of re-adding the torrent.

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
  api_token: ""
  radarr_category: radarr
  sonarr_category: sonarr
  delete_files_on_prune: false # Legacy option; local links are preserved.
```

Use Decypharr for initial imports, rearm, and provider deletion. Supply the API token from Settings → Auth when authentication is enabled; qBittorrent username/password login alone does not authenticate the provider-deletion REST endpoint.

## Provider configuration

Configure TorBox and its API key inside Decypharr. Rehydrator does not require a
TorBox API key and makes no direct TorBox calls. Both rearm and provider removal
use Decypharr. See [Decypharr lifecycle](decypharr-rearm.md).

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
  ignored_titles: []
  ignored_title_contains: []
```

Optional title exclusions apply to playback events before matching.

## Jellyfin refresh and self-heal

See [Jellyfin setup](jellyfin-setup.md) for playback webhooks and API credentials.

```yaml
jellyfin:
  enabled: true
  url: http://jellyfin:8096
  api_key: "<JELLYFIN_API_KEY>"
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_after_import: true
  refresh_after_prune: false
  refresh_delay_seconds: 45
  timeout_seconds: 20
  max_refreshes_per_run: 5
  refresh_dedupe_minutes: 10
self_heal:
  enabled: true
  interval_seconds: 300
  refresh_available: true
  recent_hours: 24
  max_refreshes_per_run: 5
```

Self-heal retries targeted Jellyfin notifications for recently restored media
without a successful refresh audit. Dedupe uses Jellyfin's independent audit log.

## Secrets

Do not commit live values for:

```text
postgres_url
radarr.api_key
decypharr.api_token
seerr.api_key
api.token
jellyfin.api_key
```

## Self-heal worker

```yaml
self_heal:
  enabled: true
  interval_seconds: 300
  jellyfin_refresh_available: true
  jellyfin_recent_hours: 24
  max_jellyfin_refreshes_per_run: 5
```

The self-heal worker periodically queues targeted Jellyfin refreshes for recently rehydrated or recently played `AVAILABLE` movies that have not had a successful Jellyfin refresh since that event. In v0.3.4, Jellyfin refresh dedupe prevents `rehydration_complete` and `self_heal_available` from refreshing the same movie folder within the configured cooldown window.

It does not scan the whole library, and it does not refresh after prune.

## Sonarr / series support

```yaml
sonarr:
  url: http://tenet-sonarr.tenet-nofear101.svc.cluster.local:8989
  api_key: "<SONARR_API_KEY>"

sonarr_sync:
  enabled: true
  interval_seconds: 60

decypharr:
  sonarr_category: sonarr

jellyfin:
  tv_section_id: 0
```

Environment equivalents:

```text
SONARR_URL=http://tenet-sonarr:8989
SONARR_API_KEY=replace_me
SONARR_SYNC_ENABLED=true
SONARR_SYNC_INTERVAL_SECONDS=60
DECYPHARR_SONARR_CATEGORY=sonarr
JELLYFIN_TV_SECTION_ID=0
```

## Jellyfin

Optional Jellyfin settings and environment overrides are documented in
[Jellyfin setup](jellyfin-setup.md). Enable `jellyfin.enabled` and supply
`jellyfin.url` and `jellyfin.api_key` to use automatic library notifications.

## Rearm and saved torrent sources

```yaml
rearm_backend: decypharr
```

`REARM_BACKEND=decypharr` is the supported backend. Native TorBox is not selected.
Apply migrations `020_saved_torrent_sources.sql`, `021_grab_source_capture.sql`,
`022_explicit_torrent_source_kinds.sql`, and `023_jellyfin_refresh_audit.sql`,
or enable `db_auto_migrate`. Enable On Grab and On Import in Arr webhook connections.
Sources are explicitly `torrent_file` or `magnet` with exactly one payload.
Hashes are identity only; missing/ambiguous sources require recovery from Arr
history or a new source capture, never automatic hash-to-magnet conversion. See [Decypharr setup](decypharr-rearm.md).
