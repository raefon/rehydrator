# Read-only TorBox consistency checks (v0.6.5)

This opt-in mode makes provider observations the primary torrent recovery signal.
It compares tracked AVAILABLE and unrequested BROKEN torrent hashes against the TorBox **account** list,
including `download_present`, rather than asking only whether a hash exists in
TorBox's global cache. Adds, rearming, queue cleanup and provider pruning remain
Decypharr operations. Rehydrator's runtime health client only issues GET requests.

## Enable

Deploy v0.6.5 with migration `027_provider_health.sql` after prior migrations,
or enable startup schema initialization. Keep `file_health.enabled` and
`rearm_enabled` enabled, then configure:

```yaml
torbox_health:
  enabled: true
  api_key: "" # Supply TORBOX_HEALTH_API_KEY through your secret configuration.
  interval_seconds: 900
  timeout_seconds: 20
```

Environment overrides are `TORBOX_HEALTH_ENABLED`, `TORBOX_HEALTH_API_KEY`,
`TORBOX_HEALTH_INTERVAL_SECONDS`, and `TORBOX_HEALTH_TIMEOUT_SECONDS`.
The Secret must belong to the **same TorBox account Decypharr uses**. No account
identity comparison between their secret configurations is performed. Enable
this only when the tracked torrents use TorBox; a mixed-debrid library needs
per-release provider attribution before this global mode is appropriate.

Checks are disabled by default, so upgrading without a key preserves the existing
filesystem-only behavior. The Kubernetes examples include an optional
`torbox-health-api-key` Secret reference. The API key is never sent in a URL or
included in diagnostic response bodies by this client.

## Comparison and recovery

The rolling file-health batch selects the targets. Torrent hashes are deduplicated
before querying and cached observations are reused for `interval_seconds` (minimum
300). A new snapshot is fetched when a selected release lacks a fresh observation;
this can happen on successive worker passes for different previously-unseen
releases. One account listing can answer all the due hashes in that pass. Pages
are paced at one request per second and the configured timeout bounds the entire
listing. Large accounts may need a larger timeout, up to 120 seconds.

The client calls `/v1/api/torrents/mylist` with `bypass_cache=true`, following
pagination until an empty page. HTTP errors, incomplete pages, malformed data,
rejected authentication, and rate limiting are not interpreted as an empty account.
No partial listing is used to infer that a tracked release is missing.

| Observation | Interpretation / action |
| --- | --- |
| `available`, `download_present` | Provider reports content present; check the mounted file. Mount-only failure does not resubmit it. |
| `unavailable`, `not_in_account` | Hash absent from a complete account listing; confirm over successive fresh observations before rearm. |
| `unavailable`, `download_not_present` | Account entry exists but content is not present; confirm before rearm. |
| `unknown` | API failed, field missing, or the download is still active. Reset provider failure confirmation and wait. |

A cached negative result is not counted as another fresh confirmation. Three
fresh negative observations are required by the default file-health threshold.
A group-level 24-hour automatic recovery cooldown prevents different episodes
from repeatedly submitting the same release. The pass's existing submission cap
still applies. Widespread negative results (three or more distinct releases at
the configured failure percentage) pause unconfirmed provider recovery for that sample. In v0.6.5, releases reaching
the fresh-negative confirmation threshold can recover even in an entirely missing
sample; API failures and unknown results reset confirmation. The submission cap
and shared cooldown still apply.
Provider-confirmed loss does not require a readable mount canary to establish
that the account placement needs restoration; filesystem outage checks still
apply to filesystem-only recovery.

When a season pack is selected for recovery, one row enters the rearm queue and
its other eligible AVAILABLE or unrequested BROKEN tracked files enter visibility waiting. Intentionally ARCHIVED
or already in-flight rows are not changed. A provider-confirmed missing placement
has its stale Decypharr queue entry cleared with `deleteFiles=false` before the
saved source is added again. Imported links remain intact.

With this mode enabled, torrent rearm completion additionally requires a positive
TorBox account observation, using a short 30-second per-release in-memory cache
shared across waiting episodes, plus the existing Decypharr/mount checks. This
avoids declaring success solely from rclone's cached file bytes. An existing
provider-available but unreadable path is left as a mount issue; optional rclone
refresh runs on its existing recovery/readiness paths rather than forcing repeated
provider adds. Use the mount/Decypharr streaming diagnostics to resolve it.

These are **provider-reported** observations, not guaranteed real-time stream
probes. TorBox can cache its own metadata; `bypass_cache=true` does not prove all
upstream state has refreshed instantly. See the
[official TorBox torrent API documentation](https://github.com/TorBox-App/torbox-sdk-go/blob/main/documentation/services/torrents_service.md).

## Inspect consistency

```bash
curl --fail-with-body \
  'http://REHYDRATOR:8080/api/health/torbox' \
  -H 'X-Rehydrator-Token: YOUR_REHYDRATOR_TOKEN'
```

This returns up to 200 stored observations, with `checked_at`, reason, torrent
ID, confirmation count, and tracked-file count. It does not perform a new API
request. Always inspect the timestamp. The stored results survive disabling the
feature, so the endpoint is an observation history view, not a live health claim.

Provider inventory never deletes media rows or overwrites source payloads/identities.
Confirmed recovery can change lifecycle states as described above. TorBox
items with no eligible Rehydrator row are not adopted or deleted; this is tracked
release reconciliation, not a two-way import of the entire account. NNTP/NZB
sources keep their filesystem/article-oriented lifecycle and are not queried as
torrent hashes. Direct cache inventory for TorBox cloud Usenet is not added.

For a library you want kept available, continue using `prune_enabled: false`;
otherwise intentional local archives remain excluded from automatic restoration.

## Broken-row recovery in v0.6.4

Provider-first sweeps include BROKEN torrents with `rearm_requested=false` and a
saved, matching torrent source. Filesystem-only mode still checks AVAILABLE rows
only. Provider-available and readable BROKEN files enter visibility waiting, where
the normal readiness worker confirms availability and notifies Jellyfin without
submitting a provider add. Provider-available but unreadable files stay BROKEN for
mount diagnostics; unknown results do not queue recovery.

Confirmed provider-unavailable releases use the existing confirmation/cooldown
and per-pass cap, with one queued add per release and shared visibility waiting
for eligible siblings. ARCHIVED, FAILED, REQUESTED, PRUNING, REARMING and
already-requested rows are not newly selected. Saved-source validation remains
required, so the number checked may be lower than the total BROKEN count.
Widespread-loss suppression still applies; this is not an unconditional bulk
restore of every broken row. No new migration beyond 027 is required.

## Missing placements after accepted adds (v0.6.7)

Visibility-waiting torrents can retry saved sources when TorBox confirms absence
and Decypharr has no job or reports a completed/error job. Active, stalled-download
and unknown queue states block retries. The first eligible observation starts a
15-minute grace period. Subsequent retries share a durable tenant/hash budget:
three attempts maximum, with 15/30/60-minute backoff. This budget does not
automatically reset after success; exhausted releases need investigation.
Migration `029_provider_retries.sql` stores reservations and the last result.
No Prowlarr fetch, synthesized magnet or provider deletion is used. Stale queue
cleanup uses deleteFiles=false. Unknown/error observations never authorize adds.
All related files continue waiting and refresh Jellyfin after becoming readable.
