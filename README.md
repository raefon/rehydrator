# Rehydrator — Decypharr re-arm + TorBox authoritative prune lifecycle controller

Rehydrator manages cached media lifecycle state for a tenant namespace.

This version uses the split that matched the live debugging session:

```text
Re-arm/add:   Rehydrator → Decypharr qBittorrent API → Radarr/Sonarr import path
Prune/delete: Rehydrator → TorBox API by infohash → provider cache removal
```

Decypharr remains the queue/import path because it is the download-client bridge Radarr/Sonarr understand. TorBox is used only for prune/dehydrate because it owns the cached provider object.

## V10 behavior change

V10 keeps the v9 provider-authoritative prune behavior and adds automation scaffolding:

- Radarr seed/sync worker: polls Radarr movies and creates/refreshes `media_cache_state` rows for imported movies.
- HTTP API on the existing health port:
  - `POST /api/rearm/movie/{radarr_id}` marks an item `ARCHIVED + rearm_requested=true`.
  - `GET /api/state` returns recent state rows for the configured tenant.
- Prune success is still TorBox/provider-authoritative by default. After TorBox confirms delete, the item is marked `ARCHIVED` even if CSI still shows the old library path.
- Re-arm still does not short-circuit on CSI visibility by default. If an item is `ARCHIVED` and `rearm_requested=true`, Rehydrator queues the torrent through Decypharr even when the path still appears in the mount.
- Old CSI-authoritative behavior can be restored with:

```yaml
prune_wait_for_csi_gone: true
rearm_short_circuit_if_csi_visible: true
```

## Flow

```text
Radarr/Sonarr history
→ Rehydrator resolves latest matching grabbed torrent
→ Rehydrator queues clean BTIH magnet to Decypharr /api/v2/torrents/add
→ Decypharr talks to TorBox/debrid provider
→ Radarr/Sonarr import and CSI-rclone/Decypharr mount expose path
→ Rehydrator marks AVAILABLE and stores infohash/magnet/source_title
→ cached_until expires
→ Rehydrator calls TorBox /api/torrents/mylist and finds torrent by infohash
→ Rehydrator calls TorBox /api/torrents/controltorrent operation=delete
→ Rehydrator marks ARCHIVED after provider delete succeeds
→ Optional CSI disappearance check can be enabled, but CSI/rclone visibility is not authoritative
```

## Important design notes

- Durable identity is `infohash`.
- `torbox_torrent_id` is optional and is filled during prune lookup when TorBox returns a matching torrent.
- Rehydrator now creates/refreshes movie rows from Radarr imports when `radarr_sync.enabled=true`. It does not yet auto-trigger re-arm from Seerr/watchlist events; use the re-arm API for that.
- Provider state is authoritative on prune. CSI/rclone can keep stale directory entries or persistent symlink paths visible after TorBox delete.
- `ARCHIVED + rearm_requested=true` queues Decypharr by default even if CSI still shows the old library path.
- Health endpoints are included:
  - `GET /healthz` → `200 ok`
  - `GET /readyz` → `200 ready`
  - `POST /api/rearm/movie/{radarr_id}` → queue re-arm for a movie
  - `GET /api/state` → list current tenant state

## Config

```yaml
postgres_url: ""
tenant: tenet-nofear101

radarr:
  url: http://tenet-radarr:7878
  api_key: ""

sonarr:
  url: http://tenet-sonarr:8989
  api_key: ""

decypharr:
  url: http://tenet-decypharr:8282
  username: ""
  password: ""
  radarr_category: radarr
  sonarr_category: sonarr
  # Kept for compatibility with older configs. V9 prune uses TorBox as the provider-authoritative delete path.
  delete_files_on_prune: true

# Required for prune/dehydrate.
torbox:
  api_key: ""

csi_path: /storage/media
health_addr: ":8080"

api:
  enabled: true
  token: ""

radarr_sync:
  enabled: true
  interval_seconds: 300

# Provider delete is authoritative; CSI/rclone can show stale library paths after prune.
prune_wait_for_csi_gone: false
# When false, ARCHIVED+rearm_requested queues Decypharr even if CSI still shows the path.
rearm_short_circuit_if_csi_visible: false

reconcile_interval_seconds: 30
csi_wait_seconds: 300
cache_grace_hours: 24
max_retries: 10
concurrent_workers: 4
db_auto_migrate: true
```

Environment variables override file config:

```bash
POSTGRES_URL=
TENANT_NAME=tenet-nofear101
RADARR_URL=
RADARR_API_KEY=
SONARR_URL=
SONARR_API_KEY=
DECYPHARR_URL=http://tenet-decypharr:8282
DECYPHARR_USERNAME=
DECYPHARR_PASSWORD=
DECYPHARR_RADARR_CATEGORY=radarr
DECYPHARR_SONARR_CATEGORY=sonarr
TORBOX_API_KEY=
CSI_PATH=/storage/media
HEALTH_ADDR=:8080
API_ENABLED=true
API_TOKEN=
RADARR_SYNC_ENABLED=true
RADARR_SYNC_INTERVAL_SECONDS=300
PRUNE_WAIT_FOR_CSI_GONE=false
REARM_SHORT_CIRCUIT_IF_CSI_VISIBLE=false
DB_AUTO_MIGRATE=true
```

## DB upgrade

If you already created tables from an earlier prototype, either set `DB_AUTO_MIGRATE=true` or run:

```bash
psql "$POSTGRES_URL" -f migrations/002_decypharr.sql
psql "$POSTGRES_URL" -f migrations/003_torbox_prune.sql
# Optional/no-op behavior note migration:
psql "$POSTGRES_URL" -f migrations/004_v9_behavior_notes.sql
# Optional/no-op v10 feature note migration:
psql "$POSTGRES_URL" -f migrations/005_radarr_sync_api.sql
```

Core columns:

```sql
infohash TEXT,
magnet TEXT,
torbox_torrent_id TEXT,
download_client TEXT DEFAULT 'decypharr',
download_category TEXT,
arr_title TEXT,
source_title TEXT
```


## Radarr seed sync and API trigger

With `radarr_sync.enabled=true`, Rehydrator polls Radarr and upserts rows for imported movies. It does not overwrite `ARCHIVED` rows back to `AVAILABLE`; archived state remains authoritative until a re-arm trigger arrives.

Check sync logs:

```bash
kubectl logs -n tenet-nofear101 deploy/tenet-rehydrator -f | grep 'radarr seed sync'
```

List state through the API:

```bash
kubectl run -n tenet-nofear101 rehydrator-api --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -s http://rehydrator:8080/api/state
```

Trigger re-arm for Radarr movie ID 1:

```bash
kubectl run -n tenet-nofear101 rehydrator-rearm --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -i -X POST http://rehydrator:8080/api/rearm/movie/1
```

If `api.token` / `API_TOKEN` is set, pass either header:

```bash
-H "Authorization: Bearer $API_TOKEN"
# or
-H "X-Rehydrator-Token: $API_TOKEN"
```

## Test commands

Pause a bad row:

```sql
UPDATE media_cache_state
SET rearm_requested = false,
    state = 'BROKEN',
    last_error = 'paused for v9 retest'
WHERE tenant = 'tenet-nofear101'
  AND media_type = 'movie'
  AND arr_id = 1;
```

Trigger a re-arm:

```sql
UPDATE media_cache_state
SET state = 'ARCHIVED',
    rearm_requested = true,
    cached_until = NULL,
    torbox_torrent_id = NULL,
    retry_count = 0,
    last_error = NULL
WHERE tenant = 'tenet-nofear101'
  AND media_type = 'movie'
  AND arr_id = 1;
```

Watch logs:

```bash
kubectl logs -n tenet-nofear101 deploy/tenet-rehydrator -f
```

Expected re-arm logs:

```text
file missing; rearming through Decypharr
# or, after prune when CSI is stale:
CSI path is visible but rearm will still be queued because CSI visibility is not authoritative for archived cache state
selected arr grabbed history ... movie_id=1 ...
decypharr add torrent request ...
rehydration complete
```

Force prune after a successful re-arm:

```sql
UPDATE media_cache_state
SET cached_until = now() - interval '1 minute'
WHERE tenant = 'tenet-nofear101'
  AND media_type = 'movie'
  AND arr_id = 1;
```

Expected prune logs:

```text
pruning expired TorBox torrent by infohash
torbox mylist request ...
torbox controltorrent delete request ...
CSI path still visible after TorBox delete; marking archived because prune_wait_for_csi_gone=false
prune complete; TorBox torrent deleted and item archived
```

If TorBox no longer has the torrent, Rehydrator treats that as archived because the provider object is already gone. CSI visibility is logged but not treated as a prune failure unless you explicitly set `prune_wait_for_csi_gone: true`.

## Build

```bash
go fmt ./...
go test ./...
go build ./cmd/rehydrator
```

If your actual repo module is `github.com/raefon/rehydrator`, update the module/import path:

```bash
go mod edit -module github.com/raefon/rehydrator
find . -type f -name '*.go' -print0 | xargs -0 sed -i 's#github.com/raefon/rehydrator#github.com/raefon/rehydrator#g'
go fmt ./...
```

## Kubernetes health service

`deploy/k8s/service.yaml` exposes the health endpoints inside the namespace:

```bash
kubectl run -n tenet-nofear101 curl-health --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -i http://rehydrator:8080/healthz
```

## v11: Seerr sync and webhook/API re-arm

This version adds Seerr integration on top of the proven lifecycle split:

- Re-arm/add: Rehydrator -> Decypharr qBittorrent-compatible API
- Prune/delete: Rehydrator -> TorBox by infohash
- Auto-seed: Radarr import sync
- Auto-rearm trigger: Seerr request sync or Seerr webhook/API POST

### Seerr sync worker

Enable the polling worker with:

```yaml
seerr:
  url: http://tenet-seerr:5055
  api_key: ""
  sync:
    enabled: true
    interval_seconds: 300
    limit: 100
```

or environment variables:

```bash
SEERR_URL=http://tenet-seerr:5055
SEERR_API_KEY=replace_me
SEERR_SYNC_ENABLED=true
SEERR_SYNC_INTERVAL_SECONDS=300
SEERR_SYNC_LIMIT=100
```

The sync worker calls Seerr's `/api/v1/request` endpoint with `X-Api-Key`, records seen requests in `media_cache_seerr_requests`, and only treats a Seerr request as a one-time re-arm signal. This avoids an infinite loop where an old persistent Seerr request re-arms an item every time Rehydrator prunes it.

### Seerr POST endpoints

The API server now accepts Seerr-style POSTs:

```bash
POST /api/seerr/webhook
POST /api/seerr/rearm
POST /api/rearm/movie/tmdb/{tmdb_id}
```

`/api/seerr/webhook` is conservative by default: it only re-arms a matching row if that row is already `ARCHIVED`, `REQUESTED`, `BROKEN`, or `FAILED`.

`/api/seerr/rearm` is the force endpoint: it can re-arm a matched movie even if the payload does not come from a poll-created request record.

Example webhook payload:

```json
{
  "mediaType": "movie",
  "tmdbId": 12345,
  "title": "Example Movie",
  "requestId": 9001
}
```

Example force payload:

```bash
curl -X POST http://localhost:8080/api/seerr/rearm \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $API_TOKEN" \
  -d '{"mediaType":"movie","tmdbId":12345,"force":true}'
```

### DB migration

Run this migration for existing databases:

```bash
psql "$POSTGRES_URL" -f migrations/006_seerr_sync.sql
```

The migration adds:

- `media_cache_state.tmdb_id`
- `media_cache_state.tvdb_id`
- `media_cache_seerr_requests`

### Suggested Seerr webhook template

Configure Seerr's webhook notification agent to POST custom JSON to:

```text
http://tenet-rehydrator:8080/api/seerr/webhook
```

If you use an API token, set Seerr's Authorization Header to:

```text
Bearer YOUR_REHYDRATOR_API_TOKEN
```

Suggested JSON payload:

```json
{
  "mediaType": "{{media_type}}",
  "tmdbId": "{{media_tmdbid}}",
  "title": "{{subject}}",
  "event": "{{notification_type}}",
  "requestId": "{{request_id}}"
}
```

Variable names can differ between Seerr versions and notification events. If a variable does not render, keep the payload simple and use `tmdbId` plus `mediaType` as the primary keys.

## v0.2.5 hardening release notes

This release hardens the working Radarr/Seerr movie lifecycle before adding Sonarr complexity.

### Added

- PostgreSQL work claiming with `FOR UPDATE SKIP LOCKED` for re-arm and prune workers.
- Exponential retry backoff via `media_cache_state.next_retry_at`.
- Provider-authoritative prune safety controls:
  - `PRUNE_ENABLED`
  - `REARM_ENABLED`
  - `MAX_PRUNES_PER_RUN`
  - `MAX_REARMS_PER_RUN`
- API token enforcement for `/api/*` by default:
  - `API_REQUIRE_TOKEN=true`
  - `API_TOKEN` is required when the API is enabled and token enforcement is on.
- Prometheus text metrics at `/metrics`.
- Structured state endpoint:
  - `GET /api/state/movie/{radarr_id}`
- Manual admin endpoints:
  - `POST /api/prune/movie/{radarr_id}`
  - `POST /api/prune/movie/{radarr_id}?dry_run=true`
  - `POST /api/rearm/movie/{radarr_id}`
  - `POST /api/refresh/radarr`
  - `POST /api/refresh/seerr`
- Seerr request audit rows now backfill `arr_id` when TMDb matches a tracked Radarr movie.

### Migration

Run:

```bash
psql "$POSTGRES_URL" -f migrations/007_hardening_api_metrics.sql
```

or use `DB_AUTO_MIGRATE=true`.

### API examples

```bash
curl -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/state
curl -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/state/movie/1
curl -X POST -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/prune/movie/1?dry_run=true
curl -X POST -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/prune/movie/1
curl -X POST -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/rearm/movie/1
curl -X POST -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/refresh/radarr
curl -X POST -H "Authorization: Bearer $API_TOKEN" http://localhost:8080/api/refresh/seerr
curl http://localhost:8080/metrics
```


## v0.2.6 playback intent re-arm

This version adds playback-triggered re-arm for Plex Pass native webhooks plus a generic playback endpoint.

New config:

```yaml
playback:
  enabled: true
  rearm_on_play: true
  cooldown_seconds: 300
```

New endpoints:

```text
POST /api/playback/plex
POST /api/playback/event
```

Plex native webhooks can call:

```text
http://tenet-rehydrator:8080/api/playback/plex?token=<API_TOKEN>
```

When a playback-start style event matches an archived movie by TMDb ID, Rehydrator records `last_play_intent_at`, increments `play_intent_count`, and requests re-arm. Available items are recorded but ignored. See `docs/plex-playback-rearm.md` for Plex and pre-roll setup notes.
