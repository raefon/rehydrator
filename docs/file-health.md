# File-health recovery (v0.6.8)

The file-health worker recovers unexpected loss of torrent-backed files that Rehydrator still
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

Since v0.6.8, scheduled file-health sampling excludes Usenet/NZB rows in both
filesystem-only and provider-first modes. Probing NNTP-backed files can initiate
article downloads, adding streaming load even for a one-byte read. Playback
recovery and Usenet rearm/visibility completion still use readability checks;
Usenet files no longer receive proactive scheduled file-health recovery.
An explicitly configured canary is still probed, so choose a torrent-backed
canary rather than an NNTP-backed file.

## What gets recovered

- Filesystem-only mode samples AVAILABLE torrent rows with a valid saved source type
  and no pending rearm. Provider-first mode also includes unrequested BROKEN
  torrents, as described below. This includes never-played episodes. ARCHIVED, FAILED, REQUESTED,
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
simultaneous filesystem failures can look like an outage. With TorBox health
enabled, v0.6.5 allows repeatedly confirmed provider losses to enter bounded
rearm recovery despite the missing percentage. Unknown API results never qualify.

In v0.6.2, an individual timeout no longer ends the sample. The health worker has
**four outstanding-read slots**, separate from rearm's read slot. Checks proceed
sequentially until a timeout leaves a read in flight; other slots allow later
files to be checked. A path already being read is never probed a second time.

If all slots are occupied, remaining targets are **deferred**. Their rolling-sweep
position advances and their previous failure counts are retained, but they do
not count as failures or successful reads and cannot trigger recovery. Outage
percentages use only attempted reads, so deferred files do not dilute an outage.

A timed-out attempted read can contribute to confirmation when the sample has a
successful read or the canary passed, subject to the widespread-failure guard.
If all four reads remain stuck forever, health checks cannot assess more files
until the mount releases them; repair/remount and restart as needed. Bounded
progress does not mean stuck kernel filesystem reads can be forcibly cancelled.

The probe still requests **one byte**, not a whole file or ffprobe metadata scan.
Opening a file alone would not establish stream readability. rclone can fetch a
larger remote chunk behind the one-byte request. ffprobe is not used because it
can seek and read more data without solving a stalled mount.

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

Apply migration `026_file_health.sql` after earlier migrations, or deploy v0.6.2
with automatic schema initialization enabled. No live deployment is performed by
updating these repository files. The worker requires `rearm_enabled: true`.

Look for `file health worker starting`, `file health pass complete`, and the
`file_health_rearm_requested` event. Summary logs include sampled, checked, timed-out, deferred, and unreadable
counts, outage suppression, and queued recoveries. Inspect persisted observations:

```sql
SELECT m.media_type, m.arr_id, m.state, h.consecutive_failures,
       h.last_checked_at, h.last_healthy_at, h.last_auto_rearm_at, h.detail
FROM media_cache_state m
JOIN media_cache_file_health h ON h.media_id=m.id
WHERE m.tenant='YOUR_TENANT'
ORDER BY h.last_checked_at DESC;
```

## Optional provider-first checks (v0.6.3)

Enable [read-only TorBox health](torbox-health.md) to compare tracked torrent
releases against the provider account inventory before making recovery decisions.
Since v0.6.4, that mode also reconciles unrequested BROKEN torrents with matching
saved sources. Archived and in-flight rows stay excluded. Provider-present mount
failures do not cause provider resubmission;
unknown API results do not imply eviction. NZB handling is unchanged.

## Recovering abandoned pruning (v0.6.6)

When pruning is disabled, the Sonarr/Radarr inventory sync can return PRUNING
torrents observed for 15 minutes with pruning disabled to BROKEN. Rearm, file health and TorBox health must
be enabled. The current Arr inventory must contain the imported file, and its
database row must have a matching saved torrent source and no pending rearm.
The normal provider health sweep then confirms availability or queues bounded
recovery. Migration `028_stale_pruning.sql` adds a durable grace-period marker; no new setting is needed. Keep inventory sync enabled.
Source-less rows and archived files remain untouched. This assumes all replicas
for the tenant use pruning disabled; drain older pruning-enabled replicas first.

## Previously archived imports (v0.6.12)

Disabling pruning does not normally reactivate archived files. To explicitly
restore previously archived torrent imports into an always-available library:

```yaml
prune_enabled: false
rearm_enabled: true
file_health:
  enabled: true
  recover_archived: true
torbox_health:
  enabled: true
```

Keep the existing TorBox health API key and Sonarr/Radarr inventory sync enabled.
Environment override: `FILE_HEALTH_RECOVER_ARCHIVED=true`.

Each current imported file observed in Arr is eligible only when it is ARCHIVED,
has no pending rearm, and has a validated saved torrent file/magnet matching its
current hash. Eligible rows become unrequested BROKEN and get an
`archived_recovery_enabled` audit event. No immediate provider add occurs.
The normal health sweep confirms loss and restores at its existing batch size,
failure threshold, max-rearms-per-pass and shared 24-hour cooldown.
Provider-present readable files move into visibility confirmation without an add.

Look for `archived import returned to provider health checks`, followed by
`file health pass complete` and the ordinary visibility/Jellyfin refresh logs.
This applies to movies and episode files. Source-less archives require source
recovery/reacquisition; Usenet and already-running jobs are unchanged. Turning
the option off prevents further transitions but does not rearchive restored rows.
No new schema migration is needed.
