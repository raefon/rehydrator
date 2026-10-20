# TorBox Pro NNTP through Decypharr (v0.6.0)

Rehydrator supports original NZB sources alongside torrent files and magnets.
Decypharr owns NNTP access, processing, streaming, and its own read cache.
Sonarr/Radarr still select releases, import files, and manage quality upgrades.

## Selected setup

Use TorBox Pro's **NNTP news server** for NZBs and keep TorBox's existing cloud
provider for torrents. Rehydrator continues to send all adds/status requests to
Decypharr. Usenet automatic pruning stays disabled. Cloud Usenet cache/CDN routing
is deferred.

Merge the `usenet` section from
[`decypharr-torbox-nntp.example.json`](../config/decypharr-torbox-nntp.example.json)
into Decypharr's existing configuration, preserving its other sections. Replace
all placeholders with the news-server details from TorBox Tools and verify the
TLS port. This file is a configuration fragment, not a complete replacement or
a template that automatically reads environment variables.

The example starts at **6 provider connections**, a **6-request streaming limit**,
and **2 processing connections per file**, leaving headroom under TorBox's
account connection budget. Processing and streaming share the provider connection
pool; the limits are not separate subscriptions. Keep one Decypharr instance for
this budget, or explicitly split connections across replicas and other NNTP apps.

Save/reload the provider configuration using Decypharr's UI, or restart Decypharr
after editing its mounted configuration. Keep the real password in your existing
secret-backed configuration rather than committing it to Git. Rehydrator needs
only its Decypharr credentials, not these news-server credentials.

## TorBox Pro: two different Usenet services

TorBox Pro's **News Server (NNTP)** can be configured as the provider in this
Decypharr integration. It does not require buying a separate NNTP subscription.
Get the hostname, TLS port, username, and dedicated news-server password from
[TorBox Tools](https://torbox.app/tools/). These are not the TorBox REST API key.
Keep them inside Decypharr, not Rehydrator.

TorBox documents a limit of **10 NNTP connections per account**. Share that budget
across all clients/replicas; do not assign 10 connections to each pod. Its news
server streams articles directly and does **not** add them to the TorBox cloud
cache/dashboard. See the [official News Server documentation](https://support.torbox.app/en/articles/15531672-torbox-news-server).

TorBox's **cloud Usenet cache/CDN API** is a separate route. The stock Decypharr
source inspected for this integration does not submit NZBs to that API; its
SABnzbd handler routes to NNTP. An open upstream [TorBox Usenet catalog PR](https://github.com/sirrobot01/decypharr/pull/431)
adds read-only mounting of already-finished cloud downloads, not NZB submission
and rearm. Do not use that PR as proof that the end-to-end cloud workflow works.
A cloud-cache implementation requires Decypharr-side submission/catalog/stream
support or another explicitly selected adapter; changing Rehydrator's provider
name alone cannot enable it.

## Configure Decypharr and Arr

1. Configure the TorBox NNTP provider in Decypharr using the fragment above and its
   [Usenet guide](https://decypharr.com/guides/usenet/overview/). Check article
   availability and playback there before enabling automated acquisition.
2. Add NZB indexers to Prowlarr and sync them to Sonarr/Radarr.
3. In each Arr, add a **SABnzbd** download client alongside the existing
   qBittorrent/Decypharr client:

   | Setting | Value |
   | --- | --- |
   | Name | Decypharr Usenet |
   | Host | `tenet-decypharr.tenet-nofear101.svc.cluster.local` (adjust for your deployment) |
   | Port | `8282` |
   | URL Base | `/sabnzbd` |
   | API Key | Decypharr API token |
   | Category | `sonarr` or `radarr`, matching the saved Arr entry in Decypharr |

   Register the Arr in Decypharr Settings → Arrs first when authentication is
   enabled. Keep Completed Download Handling enabled in Arr. See Decypharr's
   [SABnzbd integration](https://decypharr.com/guides/usenet/sabnzbd/) for alternative
   authentication methods. Rehydrator uses its existing `decypharr.api_token`
   (`DECYPHARR_API_TOKEN`), or the configured username/password, for this API.
4. Ensure Rehydrator's `decypharr.sonarr_category` / `radarr_category` exactly
   match those categories. This release uses the same categories for both
   protocols. Keep provider credentials inside Decypharr.
5. Keep authenticated Arr **Grab and Download/import webhooks** pointing to
   `/api/sonarr/webhook` and `/api/radarr/webhook`. Include all season-pack imports.

No additional SABnzbd application or direct TorBox calls are needed.
Your existing mount can remain pointed at Decypharr WebDAV. The same library and
import paths must be readable by Arr, Rehydrator, and Jellyfin.

## Deploy and verify

1. Back up PostgreSQL and deploy Rehydrator v0.6.0. Automatic schema
   initialization includes migration `025_usenet_sources.sql`; otherwise apply
   that migration after the earlier migrations before starting the new binary.
2. Grab one small NZB release through Arr. Rehydrator detects Usenet from explicit
   Arr protocol/client metadata and fetches the original NZB from the grabbed
   release URL. It does not infer Usenet from a hash or filename.
3. Confirm Arr imports it and the saved source is `nzb_file` with `protocol=usenet`.
   The original source is stored in the existing source tables with a separate
   `nzb_file` column; it is never saved as a magnet or torrent metainfo.
4. Check the file is actually readable through the mount and test Jellyfin's
   targeted scan. Existing Jellyfin refresh enable switches and limits apply.
5. Test an explicit rearm in a controlled sample. Rehydrator reuses an existing
   active/retained Decypharr job if possible. Otherwise it uploads the saved NZB
   using `/sabnzbd/api` `mode=addfile` and stores the returned opaque job ID.

Useful read-only verification queries:

```sql
SELECT m.arr_id, m.media_type, m.source_protocol, m.source_download_id,
       m.download_job_id, s.source_kind, s.nzb_filename,
       octet_length(s.nzb_file) AS saved_nzb_bytes
FROM media_cache_state m
JOIN media_cache_torrent_sources s ON s.media_id=m.id
WHERE m.tenant='YOUR_TENANT' AND m.source_protocol='usenet';
```

Initial capture needs the Arr history download URL while it remains valid.
An import without a captured source is not proof of recoverability. Keep Grab
webhooks enabled rather than relying on history surviving indefinitely. Direct
manual adds without Arr source history are not automatically recovered.

## Rearm and safety boundaries

- **Automatic Usenet pruning is disabled**, independent of `prune_enabled`.
  Rehydrator does not remove Usenet catalog entries or provider articles.
- Original Arr download IDs, current SAB job IDs, and torrent hashes are separate.
  A new NZB submission can return a different job ID. IDs are not generated from
  NZB contents and are not forced into 40-character torrent hashes.
- Season-pack episodes share a current job mapping for the exact tenant/Arr owner/
  original download ID. PostgreSQL serializes their status/submit operations.
- Completion requires SAB history `Completed` plus a real mounted-file read.
  If Arr has cleared the completed job, retained Decypharr catalog identity plus
  a real read is required. A successful add or directory listing alone is not ready.
- Polling failures wait without automatically submitting another NZB. Failed jobs
  stop with a recovery message. Missing articles/retention cannot be repaired by
  resubmitting the same NZB indefinitely; use Arr to select another release.
- NZB sources are limited to 50 MiB, validated as XML with article segments, and
  cannot be combined with a torrent file, magnet, or torrent hash. HTTP indexer
  responses containing gzip-compressed NZB files without HTTP content decoding
  are not supported in this release.
- Metadata jobs remain asynchronous. An accepted Jellyfin targeted scan does not
  mean metadata refresh has completed.
- A process/database failure between remote submission and its local checkpoint
  cannot be made atomic with the SAB API. Inspect Decypharr's queue before manually
  retrying after an uncertain submission. Keep Decypharr catalog/queue storage
  persistent alongside PostgreSQL.

This release does not automatically replace failed releases through Arr or change
Arr quality settings. Recovery and live provider availability need verification
in your deployment.
