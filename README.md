# Rehydrator

Rehydrator manages on-demand media cache lifecycles using Radarr/Sonarr,
Decypharr, PostgreSQL, CSI-rclone, and **Jellyfin**. Seerr can provide request intent.

It records imported releases, asks Decypharr to prune expired provider placements,
and restores archived media when Jellyfin playback or an API request asks for it.
Decypharr owns TorBox mutations. Optional read-only TorBox health checks compare tracked releases with the provider account inventory.

## Version

This draft is **v0.6.8**. Run `rehydrator --version` to inspect the running binary.
The Makefile embeds `VERSION`; Docker builds accept `--build-arg VERSION=0.6.8`.

## Workflow

1. Radarr/Sonarr downloads normally through Prowlarr and Decypharr.
2. Arr grab webhooks capture the exact original source before import, with durable retries.
3. Import webhooks attach that source by download ID, media owner, and exact file path.
4. After the grace period, Rehydrator preserves the source and asks Decypharr to prune.
5. Jellyfin's Playback Start webhook or a rearm API request queues restoration.
6. Rehydrator submits the saved source to Decypharr, waits for readiness and mount
   visibility, and notifies Jellyfin of the changed file path.

A saved source is explicitly a torrent file **or** a magnet, never both. The
infohash identifies the release; it is not automatically converted into a magnet.
Missing or ambiguous legacy sources require source recovery. Existing saved
sources let routine rearm bypass Arr/Prowlarr.

If Arr removes a completed queue entry, readiness can instead use the exact hash
in Decypharr's retained library plus a successful read from the mounted media file.
Keep **Remove Completed** enabled if that is your normal configuration.

## Setup

1. Copy [the configuration example](config/rehydrator.example.yaml) and fill in
   PostgreSQL, tenant, Arr, Decypharr, API authentication, and mount settings.
2. Apply the applicable SQL migrations through **023**, or enable `db_auto_migrate`.
3. Configure Arr webhooks for **On Grab** and **On Import**, including upgrades.
4. Enable Jellyfin, provide its API key, and configure its Webhook plugin.
5. Validate one movie and one episode against your running mount before broad pruning.

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

Jellyfin is the sole media-server integration. It supports movie and episode
playback, targeted automatic/manual refreshes, a manual library scan, dependency
checks, refresh history, metrics, and periodic self-heal. Paths must match the
Jellyfin filesystem view. A webhook arrives after playback starts, so the client
may need to retry once restoration finishes.

## Useful endpoints

Authenticated API requests accept `Authorization: Bearer <API_TOKEN>` or
`X-Rehydrator-Token: <API_TOKEN>`.

| Endpoint | Purpose |
|---|---|
| `POST /api/playback/jellyfin` | Jellyfin Webhook plugin playback events |
| `POST /api/radarr/webhook` | Movie grab/import/deletion notifications |
| `POST /api/sonarr/webhook` | Episode grab/import and series-deletion notifications |
| `POST /api/rearm/movie/{radarr_id}` | Request movie restoration |
| `POST /api/rearm/series/{episode_file_id}` | Request episode-file restoration |
| `POST /api/jellyfin/refresh/movie/{radarr_id}` | Notify the tracked movie path |
| `POST /api/jellyfin/refresh/series/{episode_file_id}` | Notify the tracked episode path |
| `POST /api/jellyfin/refresh/library` | Request a scan of all Jellyfin libraries |
| `GET /api/jellyfin/refreshes/recent` | Refresh audit history |
| `POST /api/self-heal/run` | Run a Jellyfin self-heal pass |
| `GET /api/health/dependencies` | Check configured services |
| `GET /metrics` | Prometheus metrics |

## Documentation

- [Jellyfin setup](docs/jellyfin-setup.md)
- [Decypharr lifecycle, source capture, and recovery](docs/decypharr-rearm.md)
- [Configuration](docs/configuration.md)
- [API](docs/api.md)
- [Migrations](docs/migrations.md)
- [Changelog](CHANGELOG.md)

## Validation and limits

Run `go test -race ./...`. PostgreSQL integration tests additionally accept
`REHYDRATOR_TEST_POSTGRES_URL` pointing to a disposable database.

Provider deletion is asynchronous inside Decypharr: acceptance does not
independently confirm TorBox deletion. Validate existing library links and STRM
setups with your installed Decypharr version. Season-pack source capture supports
multiple episode files, but group-aware provider pruning remains a limitation.

Source metainfo may contain private tracker credentials; include the source tables
in protected backups. Historical migration files and media-server audit data are
retained, while current Jellyfin scans use their own audit table.

Usenet/NZB downloads through Decypharr are supported in v0.6.0; see
[Usenet setup](docs/usenet-setup.md). Original NZBs are captured from Arr grabs;
automatic Usenet pruning remains disabled.

The selected Usenet setup uses TorBox Pro NNTP in Decypharr. See the
[provider configuration fragment](config/decypharr-torbox-nntp.example.json);
cloud Usenet cache/CDN integration is deferred.

Unexpected torrent cache loss can be recovered by [periodic file health checks](docs/file-health.md).

Optional [TorBox provider-first health checks](docs/torbox-health.md) compare stored
torrent hashes with the account's reported availability; all provider mutations
remain in Decypharr.
