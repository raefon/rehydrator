# TorBox hosted Usenet migration

Status: deferred at user request; design only, not enabled or deployed. The integration-owner choice must
be settled before implementation. Existing NNTP playback must remain operational
throughout the migration.

## Intended result

New NZBs selected by Sonarr/Radarr are downloaded and repaired in TorBox's cloud.
Playback reads completed cloud files through the media mount. Rehydrator retains
original NZB bytes and coordinates source capture, health, recovery and Jellyfin
refreshes. Indexers remain responsible for release discovery.

This differs from using TorBox's NNTP news server: news-server article streaming
does not create a cloud download.

## Integration dependency

The current Rehydrator SAB client submits to Decypharr. The inspected Decypharr
handler calls AddNewNZB, which follows its NNTP implementation. Changing account
credentials or the provider label does not route this to cloud NZB submission.

Decypharr upstream PR 431 is open and describes listing finished cloud Usenet
downloads. This is useful catalog work, but is not sufficient evidence of a full
SAB submission, job-status, import, streaming and recovery implementation.

Preferred architecture, preserving the established integration ownership:

1. Extend Decypharr's SAB pipeline with an explicit TorBox-cloud backend.
2. Have Decypharr own authenticated cloud NZB submission, status, file catalog,
   streaming-link generation, and stable mount paths.
3. Keep Rehydrator submissions going through Decypharr's SAB API.
4. Preserve NNTP as a separate backend for existing jobs.

Alternative: a separate SAB-compatible cloud adapter. This requires explicit
agreement to change integration ownership and a verified compatible mount; a
Rehydrator-only REST upload cannot make cloud files appear in Decypharr's mount.

## Durable identity and existing imports

A saved NZB source remains protocol=usenet and source_kind=nzb_file. Never convert
it into a torrent or manufacture an infohash.

Persist the selected backend on each job/source placement. Keep the original
Arr download ID, current SAB job ID, TorBox Usenet download ID, and cloud file IDs
separate. Numeric torrent and Usenet IDs can overlap and must be namespaced.

Existing NNTP jobs must continue using NNTP status and playback paths. A global
switch must not reinterpret old IDs as cloud IDs. Rehydrating an old file through
cloud storage requires an explicit placement/path migration, verified byte
readability, and Arr ownership checks. Do not bulk rewrite existing symlinks.

Season-pack submissions must share one durable job per original release and
backend, including concurrent episode recovery. Store the exact NZB upload and
filename independently from the current provider job.

## Lifecycle requirements

- Submit original NZB bytes, persist the returned job/provider identities.
- Report downloading/repairing/extracting accurately; do not mark a job completed
  until its final files can be catalogued and read.
- Present paths readable by Arr, Rehydrator and Jellyfin; preserve stable identity
  even if TorBox's display name changes.
- Use Usenet-specific cloud status and delivery operations. Never query torrent
  inventory to decide whether a hosted NZB exists.
- Treat failed or partial inventory and API errors as unknown. Apply provider
  rate-limit cooldowns and group checks by release.
- After confirmed cloud loss, resubmit the saved NZB through the selected
  integration owner with shared bounded retries.
- Refresh Jellyfin only after the final library path is readable.
- Keep automatic Usenet pruning disabled during the initial rollout.

## Verification before cutover

1. Run a separate canary category/client with one small authorized NZB.
2. Confirm it appears in TorBox's cloud dashboard and completes server-side.
3. Verify cloud-backed file reads through the mount, Arr import, and Jellyfin.
4. Remove only the canary's provider placement in a controlled test; confirm
   retained source recovery, shared job identity and final path readability.
5. Test API errors, missing cloud placement, active repair, duplicate episode
   requests, process restart and failed job reporting.
6. Enable new cloud acquisitions only after the canary passes; retain the existing
   NNTP backend until its imported library has been migrated deliberately.

This changes the acquisition/delivery backend; it does not guarantee resolution
of the torrent-stream HTTP errors already observed.

## References

- TorBox API: https://api-docs.torbox.app/
- Upstream request: https://github.com/sirrobot01/decypharr/issues/341
- Cloud catalog PR: https://github.com/sirrobot01/decypharr/pull/431
- Current NNTP setup: ../usenet-setup.md
