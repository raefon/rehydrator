# Rehydrator — Decypharr re-arm + TorBox prune cache lifecycle controller

Rehydrator manages cached media lifecycle state for a tenant namespace.

This version uses the split that matched the live debugging session:

```text
Re-arm/add:   Rehydrator → Decypharr qBittorrent API → Radarr/Sonarr import path
Prune/delete: Rehydrator → TorBox API by infohash → provider cache removal
```

Decypharr remains the queue/import path because it is the download-client bridge Radarr/Sonarr understand. TorBox is used only for prune/dehydrate because it owns the cached provider object.

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
→ Rehydrator waits for CSI path to disappear
→ Rehydrator marks ARCHIVED
```

## Important design notes

- Durable identity is `infohash`.
- `torbox_torrent_id` is optional and is filled during prune lookup when TorBox returns a matching torrent.
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
  # Kept for compatibility with older configs. V8 prune uses TorBox instead.
  delete_files_on_prune: true

# Required for prune/dehydrate.
torbox:
  api_key: ""

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
TORBOX_API_KEY=
CSI_PATH=/storage/media
HEALTH_ADDR=:8080
DB_AUTO_MIGRATE=true
```

## DB upgrade

If you already created tables from an earlier prototype, either set `DB_AUTO_MIGRATE=true` or run:

```bash
psql "$POSTGRES_URL" -f migrations/002_decypharr.sql
psql "$POSTGRES_URL" -f migrations/003_torbox_prune.sql
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

## Test commands

Pause a bad row:

```sql
UPDATE media_cache_state
SET rearm_requested = false,
    state = 'BROKEN',
    last_error = 'paused for v8 retest'
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
pruning expired TorBox torrent by infohash
torbox mylist request ...
torbox controltorrent delete request ...
prune complete; TorBox torrent deleted and item archived
```

If TorBox no longer has the torrent and the CSI path is already gone, Rehydrator treats that as archived. If TorBox does not have the torrent but the CSI path still exists, it marks the row BROKEN instead of lying about dehydration.

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
