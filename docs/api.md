# APIs

Controller routes use `Authorization: Bearer <API_TOKEN>` unless token enforcement
is explicitly disabled. Keep these on the controller Service in split deployments.
The gateway download-client routes retain their own separate credentials.

## Controller

- `GET /healthz`, `GET /readyz`
- `GET /metrics` when metrics are enabled
- `GET /api/state/summary`, `GET /api/state/movie/{radarr_id}`,
  `GET /api/state/series/{episode_file_id}`
- `POST /api/rearm/movie/{radarr_id}`,
  `POST /api/rearm/movie/tmdb/{tmdb_id}`,
  `POST /api/rearm/series/{episode_file_id}`
- `POST /api/radarr/webhook`, `POST /api/sonarr/webhook`
- `POST /api/seerr/webhook`, `POST /api/seerr/rearm`
- `POST /api/playback/jellyfin`, `POST /api/playback/event`
- `POST /api/refresh/radarr`, `POST /api/refresh/sonarr`, `POST /api/refresh/seerr`
- `POST /api/admin/self-heal/run`, `GET /api/health/dependencies`

Library rearm requests use exact saved sources and the gateway's existing recovery
flow. They do not force-upload unknown releases or bypass provider/mount readiness.
Prune routes are removed; there is no automatic provider-deletion backend.

## Gateway

- `/api/v2/`: qBittorrent-compatible Arr client, with qBittorrent credentials.
- `/sabnzbd/api`: SAB-compatible hosted-NZB client, with its separate SAB key.
- `/webdav/`: read-only shared catalog, with DAV Basic credentials. Directory
  listings use PROPFIND (Depth 0/1); files support GET/HEAD and ranges.
- `/`: dashboard. See [authentication](../web/README.md).
- `GET /dashboard/api/snapshot`: tenant-scoped jobs and catalog metadata.
- `POST /dashboard/api/jobs`: multipart magnet/torrent/NZB submission.
- `GET /dashboard/api/recovery/lookup?id=…`: exact job ID or torrent hash.
- `POST /dashboard/api/recovery/check?id=…`: bounded, observation-only provider check.
- `POST /dashboard/api/recovery/rearm?id=…`: guarded normal recovery scheduling.

Dashboard writes require the `X-Rehydrator-UI: 1` header and same-origin requests.
Native OIDC protects UI and dashboard API access without changing Arr/DAV auth.
The old separate cloud-NZB and provider-health-report endpoints are not supported.
