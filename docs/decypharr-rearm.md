# Decypharr lifecycle and saved torrent sources

Rehydrator stores lifecycle state and the exact release source. Decypharr owns
TorBox communication, downloads, and placement restoration.

## Grab capture and import linking

Keep your normal Prowlarr → Radarr/Sonarr → Decypharr download process. Configure
both Arr webhook connections to send **On Grab**, **On Import**, and import
notifications for upgrades to the existing Rehydrator endpoints:

- Radarr: `/api/radarr/webhook`
- Sonarr: `/api/sonarr/webhook`

Keep the existing Rehydrator API-token authentication on those webhooks.

On `Grab`, Rehydrator persists a capture job before network I/O and immediately
resolves that exact `downloadId` from Arr history. It saves original `.torrent`
bytes when the record contains a file URL, or the resolved full magnet when the
indexer redirects to one. File identity must match the release infohash.

Capture can precede the imported media row. The pending source is keyed by tenant,
Arr, movie/series owner ID, and download ID. The `Download` import webhook records
its exact path and download ID; sync or the reconciliation worker then attaches
the source to the imported movie or episode-file row. A season pack can attach
one captured source to multiple episode-file paths without guessing from titles.

Arr's grab webhook does not itself normally contain the download URL, so the
first capture still needs its matching history record. If history is not yet
visible or the indexer temporarily fails, the durable capture job retries after
one minute. A 202 grab response means the job was persisted, not that capture
necessarily completed. Capture retries also run when pruning/rearm is disabled.
Duplicate completed grabs do not fetch the indexer again. Pending captures are
retained; there is no automatic retention cleanup in this revision.

A newer import whose source is pending blocks rearm/prune from using an older
release's identity. Source capture is still bounded by original URL availability;
URLs that expire before any successful capture require recovery rather than
being reconstructible from a hash. Import-time history capture remains a fallback
for missed grabs, but On Grab plus On Import is the normal setup.

## Rearm

Each saved source has an explicit type, independent of the release infohash:

- `torrent_file`: original metainfo bytes plus filename, with no magnet. Rearm
  uploads only the qBittorrent-compatible multipart `torrents` field.
- `magnet`: the original magnet URI, with no torrent bytes or filename. Rearm
  submits only the `urls` field.

Mixed payloads and unresolved URLs are rejected at submission. Capture resolves
an initial URL first: a file response becomes a `torrent_file` source; a redirect
to `magnet:` becomes a `magnet` source. That is an observed response type, not
conversion of a torrent file into a magnet. The v1 infohash is calculated from
file metainfo or extracted from a magnet and verified against the release identity.

A hash alone is not a saved source. Rehydrator never constructs a magnet from a
hash. Normal rearm with a saved typed source makes no Arr/Prowlarr requests.
Missing or legacy ambiguous sources trigger an attempt to recover the original
source from Arr history. If recovery is unavailable, rearm is blocked with
`source_recovery_required`: the lifecycle row becomes `FAILED`, its rearm request
is cleared, and its hash is retained. No add is sent to Decypharr and pruning is
blocked until an original source is recovered. After recovering/capturing the
source, explicitly request rearm again.

When a queue entry exists, Decypharr must report completion before mount
visibility can mark the item available. If Arr has already removed the completed
entry, Rehydrator requires the exact hash in Decypharr's retained library plus a
successful data read from the mounted file. A stale directory entry alone is
insufficient. Incomplete/error queue entries never use this fallback. Status or
library API errors stay in the waiting lane without resubmitting the torrent.

You can keep Arr's **Remove Completed** setting enabled. The library lookup uses
the authenticated Decypharr browse API, so configure its API token even for a
rearm-only deployment with authentication enabled.

## Configuration and migration

```yaml
rearm_backend: decypharr
decypharr:
  url: http://decypharr:8282
  api_token: YOUR_DECYPHARR_API_TOKEN
  radarr_category: radarr
  sonarr_category: sonarr
```

Environment variables: `REARM_BACKEND=decypharr`, `DECYPHARR_URL`, and
`DECYPHARR_API_TOKEN`. Get the token from Decypharr Settings → Auth. Token auth
covers both qBittorrent compatibility and provider-deletion APIs. Username/password
login remains supported for qBittorrent calls; its SID cookie does not authenticate
the separate REST provider-deletion endpoint. Auth-disabled Decypharr needs no token.

Configure TorBox and its API key inside Decypharr. Rehydrator does not require
`TORBOX_API_KEY`. Native `rearm_backend: torbox` configurations are rejected.

Apply migrations `020_saved_torrent_sources.sql`, `021_grab_source_capture.sql`,
`022_explicit_torrent_source_kinds.sql`, and `023_jellyfin_refresh_audit.sql`,
or enable `db_auto_migrate` before
starting. The `media_cache_torrent_sources` table stores the source kind and exactly one
payload: metainfo as `BYTEA` (capped at 50 MiB per item), or the original magnet. Include it in database backups. Metainfo may
contain private tracker URLs/passkeys; protect those backups like other credentials.

## Pruning

Before pruning, Rehydrator ensures it has usable saved release metadata and, when
available, enriches its magnet from Decypharr's queue. It then:

1. Requests `DELETE /api/browse/torrents/{infohash}` through Decypharr. This removes
   the library/provider entry and schedules its provider placements for deletion.
2. Clears the old qBittorrent queue entry using `deleteFiles=false`, preserving
   local download links rather than removing files on disk.
3. Marks the row archived after Decypharr accepts both operations.

The qBittorrent delete endpoint alone is insufficient: it only clears the queue.
The dashboard torrent-delete endpoint can delete downloaded paths, so it is not
used for cache pruning. The legacy `delete_files_on_prune` option is ignored by
this prune path; link deletion is always disabled.

The browse provider deletion is asynchronous inside Decypharr. Rehydrator cannot
independently confirm that TorBox removed the torrent; an accepted response means
Decypharr accepted the request. Inspect Decypharr logs for provider failures.
Unsupported routes, authentication failures, and request errors fail the operation
instead of falling back to direct TorBox calls. Decypharr may remove generated
STRM entries as part of its library-entry deletion; validate STRM setups separately.

## Limits and live validation

A hash identifies a release but cannot reconstruct the original torrent file or
its tracker metadata. Migration 022 classifies saved metainfo as `torrent_file`
and extended legacy magnets as `magnet`. Legacy bare xt-only magnets and
hash-only rows are marked `recovery_required` because earlier code generated
minimal magnets and their original provenance cannot be determined. The legacy
media-row magnet remains for audit; it is not accepted as a submission source.
A newly captured genuine bare magnet is valid because its source type is known
and explicitly saved. No compatibility switch silently converts hash-only rows.

Legacy grab captures with unknown provenance become pending and retry recovery
from exact Arr history. File-backed legacy grabs have their competing magnet
cleared. Database constraints enforce exclusive payloads for ready sources.
Both infohash validation and metainfo hashing currently target v1 SHA-1 with
40-character hexadecimal identity. Original source URLs that are no longer
available cannot be reconstructed from an infohash.

Use one imported movie and episode to validate your installed Decypharr version:

1. Confirm the grab capture becomes ready in `media_cache_grab_sources` and that
   import links a `media_cache_torrent_sources` record for file-based releases.
2. Prune and check Decypharr's provider deletion logs, preserving the original
   library symlink. Confirm the torrent leaves TorBox through Decypharr.
3. Request playback/rearm. Confirm `torrent_metadata_resolved` reports source
   `database_torrent_file` or `database_magnet` and client `decypharr`.
4. Confirm the saved file is uploaded or the magnet submitted without visiting
   the old Prowlarr URL, then wait for Decypharr completion and mount visibility.
5. Remove the completed queue entry using normal Arr cleanup, then confirm
   readiness still succeeds through the matching library hash and readable file.
6. Confirm playback and the configured Jellyfin refresh work.

Shared season-pack pruning remains the existing limitation: multiple episode rows
may share one provider torrent. Validate packs separately before enabling broad pruning.

References: [Decypharr API documentation](https://decypharr.com/reference/api/),
[provider deletion handler](https://github.com/sirrobot01/decypharr/blob/main/pkg/server/api_browse.go),
and [qBittorrent deletion handler](https://github.com/sirrobot01/decypharr/blob/main/pkg/server/qbit/http.go).
