# Rehydrator — Decypharr-first cache lifecycle controller

Rehydrator manages cached media lifecycle state for a tenant namespace.

This version makes **Decypharr/qBittorrent API the primary re-arm and prune path**. TorBox is no longer called directly by the controller. Decypharr owns the download/debrid queue state; TorBox remains behind Decypharr.

## Flow

```text
Radarr/Sonarr history
→ Rehydrator resolves latest matching grabbed torrent
→ Rehydrator queues magnet to Decypharr /api/v2/torrents/add
→ Decypharr talks to TorBox/debrid provider
→ CSI-rclone/Decypharr mount exposes path
→ Rehydrator marks AVAILABLE
→ cached_until expires
→ Rehydrator calls Decypharr /api/v2/torrents/delete by infohash
→ Rehydrator marks ARCHIVED
```

## Important design notes

- Durable identity is now `infohash`, not `torbox_torrent_id`.
- `torbox_torrent_id` remains in the DB only for compatibility with earlier prototype rows.
- Rehydrator still does not create rows from Seerr/Radarr automatically. For current testing, seed `media_cache_state` after Radarr import or add a small Radarr seed worker later.
- Health endpoints are included:
  - `GET /healthz` → `200 ok`
  - `GET /readyz` → `200 ready`

## Config

```yaml
postgres_url: ""

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
  delete_files_on_prune: true

csi_path: /storage/media
health_addr: ":8080"

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
RADARR_URL=
RADARR_API_KEY=
SONARR_URL=
SONARR_API_KEY=
DECYPHARR_URL=http://tenet-decypharr:8282
DECYPHARR_USERNAME=
DECYPHARR_PASSWORD=
DECYPHARR_RADARR_CATEGORY=radarr
DECYPHARR_SONARR_CATEGORY=sonarr
DECYPHARR_DELETE_FILES_ON_PRUNE=true
CSI_PATH=/storage/media
HEALTH_ADDR=:8080
DB_AUTO_MIGRATE=true
```

## DB upgrade

If you already created tables from the TorBox-first prototype, either set `DB_AUTO_MIGRATE=true` or run:

```bash
psql "$POSTGRES_URL" -f migrations/002_decypharr.sql
```

New columns:

```sql
infohash TEXT,
magnet TEXT,
download_client TEXT DEFAULT 'decypharr',
download_category TEXT,
arr_title TEXT,
source_title TEXT
```

## Test commands

Pause a bad row:

```sql
UPDATE media_cache_state
SET rearm_requested = false,
    state = 'BROKEN',
    last_error = 'paused for Decypharr retest'
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
pruning expired Decypharr torrent
decypharr delete torrent request ...
prune complete; item archived
```

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
