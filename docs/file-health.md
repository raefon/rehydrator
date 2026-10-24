# File-health recovery (v0.6.1)

The file-health worker recovers unexpected loss of files that Rehydrator still
considers AVAILABLE. It does not depend on the episode remaining selectable in
Jellyfin. It follows the imported library path, including symlinks, and reads a
byte instead of trusting a cached directory entry.

```yaml
file_health:
  enabled: true
  interval_seconds: 300
  batch_size: 20
  failure_threshold: 3
  max_rearms_per_run: 2
  probe_timeout_seconds: 5
  outage_failure_percent: 50
  canary_path: ""
```

Every option has an uppercase `FILE_HEALTH_` environment override, such as
`FILE_HEALTH_BATCH_SIZE` and `FILE_HEALTH_CANARY_PATH`.

## What gets recovered

- Only AVAILABLE rows with a valid saved source type and no pending rearm are
  sampled. This includes never-played episodes. ARCHIVED, FAILED, REQUESTED,
  REARMING, PRUNING and WAITING_FOR_VISIBILITY rows are excluded.
- A file must fail three sampled reads by default. A successful read resets its
  failure count. A changed path or source identity starts confirmation again.
- Confirmed failures become BROKEN with `rearm_requested=true`. Existing rearm
  limits, cooldowns, source validation and provider readiness apply; the health
  worker never submits directly to Decypharr.
- At most two items are queued per default pass, and the same file/source can be
  automatically queued at most once per 24 hours. Explicit playback/API recovery
  remains available during this automatic cooldown.
- After recovery, the existing Jellyfin refresh flow can recreate missing items.
  Enable `jellyfin.refresh_after_rearm` / `refresh_after_visibility` and your
  selected refresh mode. A scan still needs a readable mount in Jellyfin.

The batch is a rolling sweep ordered by oldest observation, not a full-library
read every five minutes. For 1,000 continuously eligible files, a batch of 20 at
five minutes takes roughly 250 minutes per sweep. Three confirmations therefore
need multiple sweeps, not necessarily 15 minutes. Tune the batch deliberately;
remote reads can fetch larger chunks than the single byte requested by the probe.

## Outage protection

The worker checks Decypharr first. If it is unavailable or in a provider cooldown,
no recovery is queued. It also requires at least one successful mount read in the
sample, or a readable `canary_path` on the same remote mount. For a tiny library
where the only file can expire, configure a separate stable readable canary;
otherwise an entirely unreadable sample pauses rather than guessing.

For samples of three or more files, the default 50% failure threshold pauses the
whole sample. A canary does not override that widespread-failure guard. Large
simultaneous provider evictions can look like an outage; inspect logs and recover
selected content explicitly instead of rearming the entire library automatically.

A timed-out read stops the rest of that sample. It counts as a failure only when
the sample already has a successful read or the canary passed, and the widespread
failure guard permits recovery. Otherwise recovery pauses. At most one blocked
read remains in this worker; subsequent probes cannot accumulate additional
blocked filesystem calls.
If the mount never releases that read, repair/remount the filesystem and restart
Rehydrator as needed. These bounds cover this worker, not every reader in Arr,
Jellyfin, or other Rehydrator flows.

Failure observations, last automatic recovery, and sweep timestamps are stored
in PostgreSQL. A per-tenant transaction lock prevents replicas from performing
simultaneous sweeps. The row's current state/path/source are checked again before
queueing, so a concurrent intentional archive or upgrade wins over a stale probe.

## TorBox retention versus local pruning

TorBox describes approximately **30 days of inactivity**, with exceptions and
possible longer retention. It is not a predictable expiration date or a reason
to resubmit everything every 30 days. See its
[storage policy](https://support.torbox.app/en/articles/9961332-how-long-are-torbox-files-stored-for)
and [inactive-download explanation](https://support.torbox.app/en/articles/10333785-why-is-my-download-inactive).
TorBox Pro NNTP uses a separate article-retention model; this health check works
with both protocols without assigning a torrent retention timer to NZBs.

`cache_grace_hours` controls Rehydrator's own intentional pruning. It does not
configure TorBox retention. This release leaves its existing default of 24 hours
unchanged. If you want the whole catalog to stay available and automatically
recover unexpected cache loss, use:

```yaml
prune_enabled: false
rearm_enabled: true
file_health:
  enabled: true
```

If you intentionally want local pruning after 30 days, explicitly use
`cache_grace_hours: 720` with pruning enabled. Those items become ARCHIVED and
are intentionally not restored by health checks. They still need a playback or
API request, which may require an independent catalog if Jellyfin removes them.

A read can be served from rclone's local VFS cache, so passing a check does not
prove TorBox still has the remote payload. This is a mount-availability check,
not provider cache inventory or guaranteed keepalive. Stored source metadata and
Arr tracking must remain intact; deleted tracking rows cannot be reconstructed
by a symlink check alone.

## Deploy and inspect

Apply migration `026_file_health.sql` after earlier migrations, or deploy v0.6.1
with automatic schema initialization enabled. No live deployment is performed by
updating these repository files. The worker requires `rearm_enabled: true`.

Look for `file health worker starting`, `file health pass complete`, and the
`file_health_rearm_requested` event. Summary logs include checked/unreadable
counts, outage suppression, and queued recoveries. Inspect persisted observations:

```sql
SELECT m.media_type, m.arr_id, m.state, h.consecutive_failures,
       h.last_checked_at, h.last_healthy_at, h.last_auto_rearm_at, h.detail
FROM media_cache_state m
JOIN media_cache_file_health h ON h.media_id=m.id
WHERE m.tenant='YOUR_TENANT'
ORDER BY h.last_checked_at DESC;
```
