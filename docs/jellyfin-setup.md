# Jellyfin setup

Jellyfin is Rehydrator's media-server integration for playback, targeted refreshes,
manual library scans, dependency checks, refresh metrics, and periodic self-heal.
Decypharr owns torrent rearming; see [rearm setup](decypharr-rearm.md).

## Configuration

Create an API key in Jellyfin's Dashboard → API Keys, then configure:

```yaml
jellyfin:
  enabled: true
  url: http://jellyfin:8096
  api_key: YOUR_JELLYFIN_API_KEY
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_after_import: true
  refresh_after_prune: false
  refresh_delay_seconds: 45
  timeout_seconds: 20
  max_refreshes_per_run: 5
  refresh_dedupe_minutes: 10
```

Rehydrator authenticates requests to Jellyfin using `Authorization: MediaBrowser Token="YOUR_JELLYFIN_API_KEY"`. This is separate from the Rehydrator token used by incoming webhooks.

Every setting has a `JELLYFIN_` environment override, for example
`JELLYFIN_ENABLED`, `JELLYFIN_URL`, `JELLYFIN_API_KEY`, and
`JELLYFIN_REFRESH_AFTER_IMPORT`. Jellyfin is disabled by default.
Refreshes notify Jellyfin through `POST /Library/Media/Updated` using the tracked
file path. Paths must match Jellyfin's filesystem view. `max_refreshes_per_run`
bounds simultaneous requests, and `refresh_dedupe_minutes` suppresses repeated
automatic notifications for the same path. Audits are recorded separately in
`media_cache_jellyfin_refreshes`; a successful request means accepted, not scan complete.

Manual authenticated endpoints:

- `POST /api/jellyfin/refresh/movie/{radarr_id}` — notify the tracked movie path.
- `POST /api/jellyfin/refresh/series/{episode_file_id}` — notify the episode-file path.
- `POST /api/jellyfin/refresh/library` — request a scan of all Jellyfin libraries.
- `GET /api/jellyfin/refreshes/recent` — inspect recent notifications and dedupe skips.

Periodic self-heal uses `self_heal.refresh_available`, `self_heal.recent_hours`, and
`self_heal.max_refreshes_per_run`. A manual pass is available at
`POST /api/self-heal/run`. Metrics use `rehydrator_jellyfin_refresh_total` and
`rehydrator_jellyfin_refresh_failures_total`. Apply migration 023 before enabling
these flows, or use `db_auto_migrate`.

## Playback rearming

Install Jellyfin's official Webhook plugin and restart Jellyfin. Add a Generic
Destination with:

- Webhook URL: `http://rehydrator:8080/api/playback/jellyfin`
- Notification type: **Playback Start**
- Item types: **Movie** and **Episode**
- **Send All Properties** enabled (JSON; no custom template needed)
- Request header `X-Rehydrator-Token` set to Rehydrator's `API_TOKEN`.

If your plugin version does not allow custom headers, the existing API also
accepts `?token=YOUR_REHYDRATOR_API_TOKEN` in the webhook URL.

The endpoint accepts the plugin's `NotificationType`, `ItemType`, `ItemId`,
`Provider_tmdb`, `Name`, and username fields. Rehydrator resolves `ItemId` through
Jellyfin's API to obtain the file path. Custom payloads can supply `Path` or
`ItemPath` directly. Movie payloads without a path or enabled Jellyfin client can
match by TMDb ID. Episodes require a file path; Jellyfin item IDs are never treated
as Sonarr episode-file IDs. Stop, progress, and unrelated notifications are ignored.
The existing playback enable switch, title exclusions, and cooldown apply.

Archived items must still be visible and selectable in Jellyfin for a playback
webhook to occur. Playback Start arrives after an attempt begins; the client may
need to retry playback once rehydration completes.

API references: [Jellyfin library controller](https://github.com/jellyfin/jellyfin/blob/master/Jellyfin.Api/Controllers/LibraryController.cs)
and [official Webhook plugin](https://github.com/jellyfin/jellyfin-plugin-webhook).
