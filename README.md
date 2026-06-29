# Rehydrator

Rehydrator is an on-demand cache lifecycle manager for media stacks that use Radarr, Seerr, Decypharr, TorBox, CSI-rclone, and Plex.

It keeps Plex/Radarr library entries intact while allowing old TorBox cache entries to be pruned and re-armed later when someone tries to play the title again.

## What it does

- Tracks imported Radarr movies in Postgres.
- Prunes old TorBox cache entries after a configurable grace period.
- Re-arms archived media through Decypharr when Plex playback is attempted.
- Handles slow CSI-rclone visibility with `WAITING_FOR_VISIBILITY` instead of duplicate re-adds.
- Refreshes only the restored movie folder in Plex after a movie becomes available again to clear stale unavailable/trash indicators.
- Exposes health, API, admin, dependency-check, and Prometheus-style metrics endpoints.
- Includes a self-heal worker for recently restored movies that missed a Plex refresh.

## Current scope

Movie support is the stable path right now.

Sonarr/series support is planned, but series need a separate lifecycle model because episodes, seasons, and season packs behave differently from single movie files.

## Architecture

```text
Plex / Seerr
    ↓
Radarr
    ↓
Decypharr qBittorrent-compatible API
    ↓
TorBox cache
    ↓
CSI-rclone mount
    ↓
Plex library

Rehydrator watches and controls lifecycle state in Postgres.
```

## Lifecycle

```text
AVAILABLE
  → prune grace expires
  → TorBox torrent deleted by infohash
  → ARCHIVED
  → Plex playback webhook or API re-arm request
  → REARMING
  → Decypharr add succeeds
  → WAITING_FOR_VISIBILITY
  → CSI-rclone path appears
  → AVAILABLE
  → optional Plex refresh clears stale unavailable icon
```

## Quick start

1. Create a Postgres database for the tenant.
2. Copy `config/rehydrator.example.yaml` to `/config/rehydrator.yaml`.
3. Fill in secrets using Kubernetes Secrets or your deployment mechanism.
4. Run migrations or enable `db_auto_migrate`.
5. Deploy Rehydrator.
6. Configure Plex, Radarr, and Seerr webhooks.

Example local start:

```bash
rehydrator --config /config/rehydrator.yaml
```

## Minimal required integrations

| Integration | Purpose |
|---|---|
| Postgres | Stores media state, events, cooldowns, and audits |
| Radarr | Source of imported movie metadata and history |
| Decypharr | Re-arm/add path using qBittorrent-compatible API |
| TorBox | Provider prune/delete path by infohash |
| CSI-rclone | Mounted media visibility path |
| Plex | Playback trigger and optional library refresh |
| Seerr | Optional request/watchlist intent source |

## Config

Use this as the starting point:

```text
config/rehydrator.example.yaml
```

Do not commit live API keys, database passwords, or Plex tokens.

See [`docs/configuration.md`](docs/configuration.md) for the full config guide.

## Important Plex settings

For this workflow, keep Plex from deleting missing/archived items:

```text
Empty trash automatically after every scan: Off
Allow media deletion: Off
Video preview thumbnails: Never
Chapter thumbnails: Never
Automatic/partial scans: Off or cautious
```

See [`docs/plex-setup.md`](docs/plex-setup.md).

## API endpoints

Common endpoints:

```text
GET  /healthz
GET  /readyz
GET  /metrics
GET  /api/state
POST /api/playback/plex
POST /api/playback/event
POST /api/radarr/webhook
POST /api/seerr/webhook
POST /api/rearm/movie/{radarr_id}
POST /api/rearm/movie/tmdb/{tmdb_id}
POST /api/prune/movie/{radarr_id}
POST /api/refresh/radarr
POST /api/refresh/seerr
POST /api/plex/refresh/movie/{radarr_id}
POST /api/plex/refresh/movies
GET  /api/state/summary
GET  /api/health/dependencies
GET  /api/admin/cooldowns
POST /api/admin/retry-failed
```

See [`docs/api.md`](docs/api.md).

## Docs

- [`docs/configuration.md`](docs/configuration.md)
- [`docs/lifecycle.md`](docs/lifecycle.md)
- [`docs/plex-setup.md`](docs/plex-setup.md)
- [`docs/radarr-seerr-setup.md`](docs/radarr-seerr-setup.md)
- [`docs/torbox-decypharr.md`](docs/torbox-decypharr.md)
- [`docs/troubleshooting.md`](docs/troubleshooting.md)
- [`docs/api.md`](docs/api.md)
- [`docs/migrations.md`](docs/migrations.md)

## Safety defaults

The bundled example config favors provider safety over speed:

```text
concurrent_workers: 2
max_rearms_per_run: 3
max_prunes_per_run: 5
provider_cooldown_seconds: 900
csi.visibility_timeout_seconds: 900
```

That is intentional for TorBox/WebDAV/CSI-rclone stacks.

Plex refreshes are targeted by default: Rehydrator scans the restored movie folder path first, not the whole movie library. Whole-section scans are available only as a manual repair endpoint.
