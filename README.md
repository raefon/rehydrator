# Rehydrator

Postgres-first rehydration/dehydration controller for:

- Jellyseerr/Overseerr
- Radarr/Sonarr
- Decypharr
- TorBox
- CSI-Rclone
- Plex

This skeleton assumes Radarr/Sonarr create the durable symlink/library entry and TorBox is only the temporary cache layer.

## Core idea

Radarr/Sonarr history is the torrent metadata source of truth.

Postgres stores only cache lifecycle state:

- should this item be rearmed?
- is it currently available?
- when should it be pruned?
- what TorBox torrent ID should be deleted?

## Flow

```text
Plex watchlist
  -> Seerr
  -> Radarr/Sonarr
  -> Decypharr
  -> TorBox
  -> CSI-Rclone
  -> persistent symlink/library entry

After grace period:
  -> Rehydrator deletes TorBox torrent
  -> state = ARCHIVED
  -> symlink remains

Later:
  -> rearm_requested = true
  -> Rehydrator queries Arr history
  -> extracts torrentInfoHash/guid
  -> re-adds torrent to TorBox
  -> waits for CSI-Rclone path
  -> state = AVAILABLE
```

## Database

Manual init:

```bash
psql "$POSTGRES_URL" -f migrations/001_init.sql
```

Or app-managed init:

```bash
DB_AUTO_MIGRATE=true go run ./cmd/rehydrator
```

## Environment

```bash
POSTGRES_URL=postgres://user:pass@postgres:5432/tenantdb?sslmode=disable

RADARR_URL=http://radarr:7878
RADARR_API_KEY=replace_me

SONARR_URL=http://sonarr:8989
SONARR_API_KEY=replace_me

TORBOX_API_KEY=replace_me

CSI_PATH=/storage/media

RECONCILE_INTERVAL_SECONDS=30
CSI_WAIT_SECONDS=180
CACHE_GRACE_HOURS=24
MAX_RETRIES=10
CONCURRENT_WORKERS=4
DB_AUTO_MIGRATE=false
```

## Build

```bash
docker build -t rehydrator:dev .
```

## Run

```bash
go run ./cmd/rehydrator
```

## Seed a re-arm item

```sql
INSERT INTO media_cache_state (
  tenant,
  media_type,
  arr_id,
  symlink_path,
  state,
  rearm_requested
)
VALUES (
  'tenet-nofear101',
  'movie',
  2,
  '/storage/media/movies/Attack on Titan - Wings of Freedom (2015)/[Maximus] Attack on Titan Movie 2 - Wings of Freedom (2015) [BluRay 1080p x265 10bit AC3 5.1].mkv',
  'ARCHIVED',
  true
)
ON CONFLICT (tenant, media_type, arr_id)
DO UPDATE SET
  symlink_path = EXCLUDED.symlink_path,
  state = EXCLUDED.state,
  rearm_requested = EXCLUDED.rearm_requested;
```

## Mark an item for re-arm

```sql
UPDATE media_cache_state
SET rearm_requested = true,
    state = 'ARCHIVED'
WHERE tenant = 'tenet-nofear101'
  AND media_type = 'movie'
  AND arr_id = 2;
```

## Notes

- `arr_id` is Radarr `movieId` for movies.
- Sonarr is series-level in this skeleton. Episode-level support can be added later with `episode_id` or `download_id`.
- The TorBox delete endpoint is a best-effort skeleton. Adjust it if your API wants a different route or hash-based deletion.
- The pruner only deletes when `state = AVAILABLE`, `rearm_requested = false`, `cached_until < now()`, and `torbox_torrent_id IS NOT NULL`.
- This avoids a prune/re-arm loop.
