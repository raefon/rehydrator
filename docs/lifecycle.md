# Lifecycle

Rehydrator tracks each movie in `media_cache_state`.

## Main states

| State | Meaning |
|---|---|
| `REQUESTED` | Placeholder from Seerr or early request intent |
| `AVAILABLE` | Imported and visible/expected to be playable |
| `ARCHIVED` | Provider cache has been pruned; library entry remains |
| `REARMING` | Re-arm has been requested and Decypharr add is in progress |
| `WAITING_FOR_VISIBILITY` | Provider add succeeded, waiting for CSI-rclone path visibility |
| `FAILED` / `BROKEN` | Real failure requiring attention |

## Normal prune flow

```text
AVAILABLE
→ cached_until expires
→ TorBox torrent deleted by infohash
→ ARCHIVED
```

Rehydrator does not require CSI-rclone to stop showing the path after prune, because the mount can be stale.

## Normal re-arm flow

```text
ARCHIVED
→ playback intent or API rearm
→ REARMING
→ Decypharr add succeeds
→ WAITING_FOR_VISIBILITY
→ CSI path appears
→ AVAILABLE
```

If the CSI path is slow, Rehydrator retries visibility checks without duplicate Decypharr add calls.

## Plex hygiene flow

```text
WAITING_FOR_VISIBILITY → AVAILABLE
→ delayed Plex refresh
→ Plex clears stale unavailable/trash icon
```

Do not enable Plex automatic trash emptying.

## Event-driven seeding

Seerr and Radarr events reduce the race where playback happens before Rehydrator has a DB row:

```text
Seerr request → REQUESTED placeholder
Radarr import/webhook → real Radarr arr_id/path/infohash
Plex playback → tracked play intent and optional re-arm
```
