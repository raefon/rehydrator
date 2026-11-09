# Rehydrator v0.9.1 — Recover uncertain NZB submissions (unreleased)

- Mark new NZB uploads with their gateway job identity and reconcile timed-out submissions against a unique TorBox placement, without uploading again. Preserve original NZB bytes and hide transport markers from import names.
- Prevent duplicate provider bindings and cross-tenant adoption. Older unmarked submissions and ambiguous matches remain unresolved for manual review; recovery checks run every five minutes.
- Cross-check the full TorBox Usenet inventory when a per-ID response reports finished/100% but has not exposed its files. Accept only the same ID with a ready, valid file catalog. No configuration or schema migration required.

# Rehydrator v0.9.0 — Separate standard and 4K controller libraries (unreleased)

- Add controller-only library_edition: 4k to isolate Arr state/recovery IDs under an internal tenant:4k scope while sharing the original tenant's gateway, sources and WebDAV mount. Existing standard identities remain unchanged.
- Add gateway.additional_categories for sonarr-4k/radarr-4k and library_paths for edition-specific playback handling.
- Filter Seerr request reconciliation by edition; verify webhook request editions through Seerr and optionally forward 4K requests using seerr_4k_controller_url.
- Include both editions in gateway recovery, diagnostics and library-reference deletion guards. Display the 4K controller as a separate shared-log source. No schema migration beyond 036.

# Rehydrator v0.8.9 — Confirmed deletion of selected downloads (unreleased)

- Allow checkbox selection and confirmed deletion of queued, downloading, mounting, missing and completed jobs as well as failed jobs.
- Remove the requirement that a TorBox placement be failed before explicitly deleting it. Retain jobs when provider lookup/deletion fails.
- Preserve library/shared-placement protections and block deletion while a submission is in flight or its provider identity is unresolved. No schema migration required.

# Rehydrator v0.8.8 — Unified live Gateway and Core logs (unreleased)

- Replace the recent-log table with a terminal-style window combining Gateway and Core output, with source/level filters, search, pause/resume and follow-newest. Refresh every two seconds and stop following when scrolling back.
- Share bounded, sanitized process snapshots through PostgreSQL for split deployments, merge up to 600 entries per tenant, and report available sources. Fall back to local logs when the shared feed is unavailable; preserve dashboard authentication and log redaction.
- Apply migration 036 before upgrading Gateway and Core, or use database auto-migration. Combined deployments display a Combined source label. Expire inactive sources after ten seconds and clean up old snapshots after ten minutes.

# Rehydrator v0.8.7 — Dashboard logs and confirmed failed-job deletion (unreleased)

- Add a recent operational log view with search and level filters, backed by a bounded process-local buffer. Exclude free-form attributes such as errors, response bodies and credentials.
- Select up to 50 failed jobs and confirm deletion from both TorBox and Rehydrator, with per-job results. Keep records when provider checks/deletion fail; reject active, library-linked and shared provider placements.
- Preserve original submission sources and library records. No automatic pruning, provider-wide deletion, Kubernetes API access or schema migration.

# Rehydrator v0.8.6 — Release-named torrent import folders (unreleased)

- Extend pinned release-named staging to torrents so Sonarr can parse episode and season names instead of opaque job IDs.
- Keep qBittorrent save paths, content paths and relative file paths consistent for modern and legacy Arr clients.
- Re-verify visible completed torrents without re-uploading; retain old staging links and stable WebDAV paths. Upgrade gateway and controller together. No schema migration required.

# Rehydrator v0.8.5 — Discover unindexed Jellyfin parent folders (unreleased)

- Fall back to a native Jellyfin folder-change notification when TargetedScan reports ParentNotFound, including HTTP 404 responses carrying that status.
- Apply the fallback to movie and episode refreshes; discovery remains asynchronous and does not trigger a full library scan.
- Keep missing-plugin, missing-file and other scan failures visible. No schema migration required.

# Rehydrator v0.8.4 — Report terminal provider download failures to Arr (unreleased)

- Recognize decorated TorBox failed/aborted states, including NZBs that cannot be completed, instead of leaving them Downloading.
- Report terminal NZB failures as SAB history Failed entries with a sanitized fail_message and stable download identity, enabling Arr's configured native failed-download blocklisting/replacement search.
- Keep network errors, uncertain submissions, processing and mount delays separate from release failures. Preserve Arr's disk/write-error warning special case.
- Retain original sources and job history after Arr removes failed client entries. No schema migration beyond 035 required.

# Rehydrator v0.8.3 — Match the shared Authentik theme (unreleased)

- Match Authentik's supplied white/charcoal surfaces, rounded cards, soft controls and shadows while retaining Rehydrator's green accent.
- Add per-host light/dark/system selection with system-change tracking and a stored visual preference; authentication and tenant data are unaffected.
- Align the standalone OIDC denial page with the same neutral palette using OS light/dark preference, retaining its script-free HTTP 403 behavior.

# Rehydrator v0.8.2 — OIDC access-denied page (unreleased)

- Replace the plain account-denied response with an accessible, responsive Rehydrator page and sign-in retry link.
- Keep HTTP 403 and the existing authorization checks; no token, user or group details are exposed. The page is embedded and needs no frontend assets or external requests.

# Rehydrator v0.8.1 — Release-named SAB import paths (unreleased)

- Persist sanitized release folders for NZB staging and report them as SAB history storage, keeping opaque job IDs as stable internal identity.
- Verify the release-named staging path before announcing completion; freeze its name across provider polling, restarts and recovery.
- Re-verify existing visible completed NZBs without re-downloading or deleting old staging links. Torrent staging and WebDAV paths are unchanged.
- Apply migration 035 before new binaries, or start the schema-migrating gateway before upgrading the controller.

# Rehydrator v0.8.0 — Direct gateway consolidation (unreleased)

- Remove Decypharr clients and lifecycle operations, the separate cloud-NZB backend/routes, provider pruning APIs, and the superseded periodic file/provider-health worker.
- Keep gateway/controller roles plus combined mode on one direct TorBox backend for torrents and hosted NZBs. Rename current gateway internals to protocol-neutral names.
- Move categories to gateway.radarr_category / gateway.sonarr_category with GATEWAY_RADARR_CATEGORY / GATEWAY_SONARR_CATEGORY overrides.
- Replace legacy configs, environment samples, Kubernetes examples and setup guides. Unknown/removed YAML fields and nonempty retired environment variables now fail with migration guidance.
- Preserve the Arr missing-file canary safeguard, original sources, stable job IDs, current WebDAV paths, database records and all historical SQL migrations. No schema migration beyond 034 is required; no tables or provider data are deleted.

# Rehydrator v0.7.10 — Configurable active provider polling (unreleased)

- Add gateway.active_poll_interval_seconds / GATEWAY_ACTIVE_POLL_INTERVAL_SECONDS, default 30 seconds, with a 15–3600 second range.
- Apply the interval after submission and while torrents/NZBs download or process. Preserve completed-job checks, mount verification, error delays, and submission spacing.
- Log the effective interval at worker startup. No schema migration beyond 034 is required.

# Rehydrator v0.7.9 — TorBox provider progress (unreleased)

- Persist whitelisted provider observations from existing polling: original state, optional download fraction, speed, size, downloaded bytes and completion/presence/cache flags.
- Show TorBox progress and readiness separately from Rehydrator workflow state. Preserve unknown values, processing-at-100%, and stale observations after lookup failures.
- Clear observations when provider placements change; browsing the dashboard adds no provider polling requests.
- Apply migration 034 before rolling out gateway/controller binaries, or use gateway db_auto_migrate before controller rollout.

# Rehydrator v0.7.8 — Recovery and diagnostics dashboard (unreleased)

- Add tenant-scoped torrent-hash/job lookup, retained-source validation, provider IDs, scheduled checks, attempt budgets, and matching library records.
- Add bounded observation-only provider checks and guarded recovery scheduling through the existing worker. Unknown submissions, failed jobs, exhausted budgets, disabled recovery and unavailable sources cannot be force-retried.
- Show retained download history even after Arr hides jobs from its client queue. Pre-gateway downloads added directly to TorBox are not imported as historical jobs.
- No schema migration required.

# Rehydrator v0.7.7 — Native OIDC dashboard login (unreleased)

- Add Authentik-compatible OIDC discovery, authorization code with PKCE, ID-token verification, nonce/state binding and optional group restrictions.
- Protect dashboard assets and APIs with short-lived secure sessions; support local logout and session-expiry reauthentication without falling back to Basic or proxy headers.
- Retain mutually exclusive ForwardAuth and Basic dashboard modes. Download-client and WebDAV authentication is unchanged. No schema migration required.

# Rehydrator v0.7.6 — Gateway dashboard (unreleased)

- Serve an embedded React/TypeScript dashboard from the gateway, with live tenant-scoped jobs, catalog browsing, and durable magnet/torrent/NZB submissions.
- Support existing Authentik ForwardAuth through opt-in external dashboard authentication; keep download-client and WebDAV credentials unchanged.
- Poll stored catalog state without media reads or provider calls, and display pending controller verification without invented progress or health measurements.
- Add a frontend build stage to the container and a release-build Make target. No schema migration required.

# Rehydrator v0.7.5 — Hosted-NZB client integration checks (unreleased)

- Align SAB fullstatus with the completedir field expected by Arr and explicitly report an empty sorter list, disabled sorting and retained history.
- Log durable NZB acceptance without exposing source contents or credentials.
- Identify TorBox queue-only NZB responses without mistaking queued IDs for active Usenet placements or permitting blind duplicate uploads.
- Add Sonarr/Radarr-style SAB request tests for separate categories, original NZB retention, split-role completion, stable shared-mount paths and history cleanup.
- Document a live hosted-NZB acceptance procedure using the existing gateway and CSI mount; no separate NNTP/SAB server or schema migration is required.

# Rehydrator v0.7.4 — Correct qBittorrent import paths (unreleased)

- Report the category staging directory as qBittorrent save_path and the stable per-job directory as content_path, avoiding Sonarr's base-download-directory import warning.
- Prefix qBittorrent file names with the job directory so files/properties reconstruct the same import location. Existing staged files, sources and WebDAV paths are unchanged; no redownload or migration is needed.
- Persist the provider's observed download name when updating job status.
- Add contract regressions for Sonarr's modern content-path check and legacy files/properties path reconstruction across single-file, season-pack and loose-file catalogs.

# Rehydrator v0.7.3 — Adopt verified provider matches without exhaustive pagination (unreleased)

- Torrent preflight and unknown-submission reconciliation may use a validated exact hash/ID match immediately, avoiding an unnecessary next-page request that can repeat the first page and block adoption.
- Full provider health scans still require complete inventory. Missing hashes and repeated/partial pages remain inconclusive; they never authorize an eviction or another upload.
- Save the current unknown-submission lookup error in the job record rather than leaving only an old add-response error visible.
- No schema changes or retry-budget resets. Existing SubmissionUnknown jobs remain observation-only.

# Rehydrator v0.7.2 — Start provider transfers normally (unreleased)

- Fix torrent/NZB submission accidentally forcing TorBox queue-only mode with `as_queued=true`; request normal scheduling instead.
- Recognize queue-only torrent responses without treating a queue ID as an active torrent ID. Retain the unknown-outcome guard so accepted queued jobs are not blindly resubmitted.
- Log preflight submission deferrals, including provider account lookup failures.
- Read-only reconciliation can attach an existing active torrent to a SubmissionUnknown job by exact account/hash match. It never resets attempts or submits another copy. Queue-only items must first start in TorBox.

# Rehydrator v0.7.1 — Separate gateway and controller roles (unreleased)

- Add `role: gateway|controller|combined`, `REHYDRATOR_ROLE`, and a `--role` override. Combined remains the legacy-compatible default.
- Gateway serves provider/client APIs and WebDAV without accessing CSI or staging paths, starting Arr/Jellyfin workers, or requiring controller credentials. Readiness checks its database catalog.
- Tenant controllers create staging links and verify mounted payloads using separate durable leases; catalog revisions prevent stale verifications from completing replaced/missing placements. Migration 033 adds verification and deferred-cleanup state.
- Queue delete-data requests in gateway mode enqueue controller cleanup instead of touching local paths. Sources, library files and provider payloads remain retained.
- Add per-role configuration and cross-namespace Kubernetes examples, with one gateway per tenant and shared tenant database. No central multi-tenant routing or UI.

# Rehydrator v0.7.0 — TorBox gateway playground (unreleased)

## Direct gateway
- Opt-in direct TorBox torrent and hosted-NZB jobs share a durable worker, original-source store and authenticated `/webdav/` catalog. No Decypharr calls in direct acquisition/recovery mode. UI remains deferred.
- qBittorrent-compatible `/api/v2/` supports Arr login, magnet/file submission, queue/files/properties/categories and local completion cleanup. SAB uses `/sabnzbd/api`.
- Migration 032 retains sources independently of imported media and tracks expiring hashed client sessions. Recovery keeps stable per-job torrent/Usenet paths after provider ID changes.
- Direct-mode Arr inventory binds imports through gateway symlinks. Confirmed provider loss wakes related imports for visibility completion and Jellyfin refresh.
- Direct mode rejects provider pruning and unsupported torrent controls. V2/hybrid torrents and remote torrent URL fetching are excluded. Ambiguous submissions require inspection.
- Added gateway configuration, CSI remote example, redacted job diagnostics, operational setup and PostgreSQL/mock-provider lifecycle tests. Live TorBox/CSI/Arr/Jellyfin acceptance remains deployment work.

## Added
- Opt-in SAB-compatible /sabnzbd-cloud/api accepts original NZBs and durably submits them directly to TorBox's cloud Usenet API, including server-side repair/unpacking.
- Read-only authenticated /cloud-usenet/dav/ exposes stable per-job paths for a separate rclone/CSI mount. Range streams proxy cached, short-lived CDN links without exposing the TorBox key or making an API call per range read.
- Migration 031_cloud_usenet.sql stores original NZBs, namespaced client job IDs, separate provider IDs, file catalogs, submission reservations, shared upload throttling and bounded recovery attempts.
- Staging symlinks and SAB completion support normal Arr imports. Grab capture preserves cloud sources locally; cloud jobs use their own status/readability for rearm and Jellyfin refresh.
- Three fresh missing-placement observations can queue saved-NZB recovery when cloud recovery and rearm are enabled. Related imports enter shared visibility waiting. Interrupted/ambiguous submissions stop for inspection rather than blindly duplicating.
- Legacy-mode Decypharr NNTP imports and torrents retain their backend. No automatic old-path migration, Usenet pruning or routine NNTP health reads.

# Rehydrator v0.6.12 — Opt-in archived import recovery (unreleased)

## Added
- `file_health.recover_archived` (default false) brings current Arr-owned archived torrent imports with validated matching saved sources back into provider health reconciliation when pruning is disabled.
- Transition to unrequested BROKEN, not AVAILABLE or an immediate provider add. Existing health batch limits, fresh confirmations, per-release deduplication, recovery caps and cooldowns govern restoration.
- Skip source-less, mismatched/invalid sources, Usenet and in-flight rows. Retain metadata and write an audit event per transition. No new migration required.

# Rehydrator v0.6.11 — Seerr TV recovery and individual Arr missing files (unreleased)

## Added
- Accept TV requests through Seerr webhooks and polling, including Moonfin requests forwarded through Seerr. Only approved requests with explicit TVDB identity and season scope rearm matching saved sources. One primary submission per release; siblings await visibility.
- Deduplicate TV requests across webhook/polling using the Seerr request ID. Pending approvals, available content, unrequested seasons and imports without saved sources are not rearmed.
- Opt-in `jellyfin.inventory_sync.mark_missing_in_arr` updates individual Arr imported-file records for source-less failed torrents with confirmed dangling symlinks. Requires a readable same-mount canary, fresh Arr ownership/path checks, and rechecks absence immediately before the file-delete API.
- Keep acquisition with Seerr/Sonarr/Radarr. No automatic replacement search, unmonitoring or blocklisting. Default behavior preserves Arr files; no schema migration required.

# Rehydrator v0.6.10 — Flag missing imports without recovery sources (unreleased)

## Fixed
- Jellyfin inventory reconciliation checks path metadata for source-less torrent imports missing from Jellyfin. A confirmed missing path moves the unchanged idle import to FAILED with an explicit source-recovery-required reason, rather than repeatedly submitting impossible targeted scans.
- Valid saved sources and concurrent source/lifecycle changes are preserved. Existing files remain scan candidates; absence from Jellyfin alone does not establish file loss.
- Metadata checks are bounded and never open/read media bytes. Usenet imports bypass these checks. No migration, re-download, provider deletion or automatic Arr blocklisting is introduced.

# Rehydrator v0.6.9 — Reconcile missing Jellyfin movies and episodes (unreleased)

## Added
- Periodic metadata-only comparison of the synchronized Arr import inventory with Jellyfin movie/episode paths, including alternate versions and optional directory-prefix mappings.
- Queue at most five targeted scans per default 30-minute pass for AVAILABLE imports missing from the index, including older and never-played files without saved rearm sources.
- Durable per-item six-hour scan-attempt cooldowns across replicas/restarts, refresh audit entries, and pass summary logs. Incomplete/failed Jellyfin inventories abort the pass.
- Migration `030_jellyfin_inventory.sql` and YAML/environment controls. Requires targeted_scan mode and its Jellyfin plugin. No direct Usenet read probes, provider re-adds, lifecycle resets or full-library scan fallback.

# Rehydrator v0.6.8 — Torrent-only scheduled health checks (unreleased)

## Changed
- Exclude Usenet/NZB rows from scheduled file-health selection in filesystem-only and TorBox provider-first modes, avoiding background NNTP reads from routine scans.
- Retain Usenet playback recovery and rearm/visibility readability checks. Existing queued jobs are unchanged; proactive Usenet file-health recovery is disabled.
- Log the torrent-only source scope at worker startup. No migration or configuration change is required.

# Rehydrator v0.6.7 — Retry missing provider placements (unreleased)

## Fixed
- Waiting torrent releases absent from TorBox can retry their exact saved source through Decypharr after a 15-minute observation grace period.
- Migration `029_provider_retries.sql` reserves retries durably per tenant/hash before submission. At most three retries use 15/30/60-minute backoff; restarts and sibling episodes share the budget.
- Active or unknown Decypharr queue states, provider API errors, unknown provider results and invalid saved sources do not trigger resubmission. Queue cleanup preserves files. Waiting episodes continue normal visibility and Jellyfin refresh handling.

# Rehydrator v0.6.6 — Recover abandoned pruning (unreleased)

## Fixed
- With pruning disabled and rearm, file health and TorBox health enabled, current Arr inventory returns eligible PRUNING torrents to BROKEN after a durable 15-minute observation window. Matching saved sources are required; the bounded provider health worker handles recovery. Migration `028_stale_pruning.sql` adds the observation marker.
- Preserve archives, source-less files, recent operations and in-flight requests. No provider deletion, immediate add or blind AVAILABLE reset occurs.

# Rehydrator v0.6.5 — Recover confirmed provider losses (unreleased)

## Fixed
- Allow repeatedly confirmed TorBox losses to enter the existing Decypharr rearm flow even when most sampled releases are missing. Cached observations do not add confirmations; API errors and unknown results reset them.
- Preserve per-pass recovery caps, shared release deduplication, 24-hour cooldowns, source validation and filesystem outage protection.

# Rehydrator v0.6.4 — Reconcile unrequested broken torrents (unreleased)

## Fixed
- Include unrequested BROKEN torrent rows in provider-first health sweeps when TorBox health is enabled, retaining source validation, confirmation thresholds, outage suppression and recovery caps.
- Queue one missing-release restore and route eligible broken siblings into shared visibility waiting. Already provider-present, readable broken files enter visibility confirmation without a provider add.
- Archived, failed, requested, pruning, rearming, source-less and already-requested rows remain excluded from new automatic work. No bulk lifecycle reset or schema migration is introduced.

# Rehydrator v0.6.3 — TorBox provider-first health (unreleased)

## Added
- Opt-in read-only TorBox account checks compare tracked torrent hashes with complete paginated account inventory and `download_present`. Missing entries, unavailable payloads, and unknown API results are stored separately from media/source records.
- Migration `027_provider_health.sql` retains timestamped per-release observations, confirmation counts and shared recovery cooldowns. GET `/api/health/torbox` reports stored observations.
- Group checks and recovery by torrent hash. One season-pack rearm is submitted through Decypharr; related AVAILABLE files wait for provider readiness and readable mount data.

## Changed
- With TorBox health enabled, mount-only failures do not resubmit provider-available torrents; unknown results do not imply eviction. All provider mutations still go through Decypharr. NNTP retains its existing health flow.

# Rehydrator v0.6.2 — Health-scan progress past slow files (unreleased)

## Fixed
- Continue a file-health sample after individual one-byte read timeouts, using a separate four-slot outstanding-read budget. Never launch duplicate concurrent probes for the same path.
- Record busy/skipped probes as deferred observations, not unreadable-file evidence; advance rolling scan order while preserving prior failure counts.
- Keep health reads separate from rearm probes and log checked, deferred, and timed-out counts. Existing outage guards and recovery limits remain in force.
- Avoid retrying an identical CSI path through the configured root alias.

# Rehydrator v0.6.1 — Automatic file-health recovery (unreleased)

## Added
- Rolling, bounded file-read probes for AVAILABLE media with saved sources: repeated unreadability queues the existing rearm flow without needing a Jellyfin playback event.
- Durable confirmation counts, per-tenant sweep coordination, at most two automatic queues per default pass, and a 24-hour per-file/source automatic recovery cooldown.
- Decypharr health checks, timeout protection, widespread-failure suppression, and an optional mount canary. Archived/in-flight items are excluded.
- Rearm completion and the optional visible-path shortcut now require actual readable bytes using bounded probes, rather than stale file metadata.
- Migration `026_file_health.sql`, configuration/env options, and availability-versus-retention guidance. Local pruning policy remains unchanged; no unconditional 30-day rearm timer.

# Rehydrator v0.6.0 — Usenet support and TorBox Pro NNTP (unreleased)

## Added
- Decypharr provider configuration fragment for TorBox Pro's NNTP news server, with a conservative shared connection budget and setup instructions.
- Document the selected routing: torrent cloud access and direct TorBox NNTP for NZBs, with credentials held in Decypharr and Usenet pruning disabled. Cloud Usenet cache/CDN routing is deferred.
- Capture and retain original NZB bytes from explicit Arr Usenet grabs, with import associations for movies and season-pack episodes.
- Rearm via Decypharr's SABnzbd-compatible API, preserving opaque runtime job IDs separately from torrent hashes and original import IDs.
- Share current NZB job mappings across season-pack episodes with database serialization; poll existing jobs instead of repeatedly submitting.
- Migration `025_usenet_sources.sql` adds protocol-aware source storage and runtime job mappings. Existing torrent sources remain valid.

## Changed
- Usenet is excluded from automatic pruning. Failed NZB jobs require recovery rather than automatic resubmission of missing articles.
- Usenet readiness requires a completed job or retained catalog identity plus a readable mounted file. Targeted Jellyfin notifications for known Usenet imports are gated on readability.

# Rehydrator v0.5.11 — Jellyfin Targeted Scans support (unreleased)

## Added
- Optional `jellyfin.refresh_mode: targeted_scan` calls the Targeted Scans plugin directly and validates Created/Refreshed results. No full-library fallback.

## Fixed
- Describe successful refresh requests as accepted rather than completed scans.

# Rehydrator v0.5.10 — Legacy lifecycle constraint repair (unreleased)

## Fixed
- Repair existing database state constraints during schema initialization so successful rearm submissions can enter `WAITING_FOR_VISIBILITY`. Standalone migration: `024_repair_lifecycle_state_constraint.sql`.

# Rehydrator v0.5.9 — Jellyfin playback user context (unreleased)

## Fixed
- Log incoming Jellyfin playback starts and lookup failures; ignored non-start notifications are logged at debug level.
- Pass the playback webhook user ID into Jellyfin item lookups to avoid empty-user GUID errors with API-key authentication.

# Rehydrator v0.5.8 — Jellyfin authentication header (unreleased)

## Fixed
- Authenticate Jellyfin item lookup, health checks, and library refreshes using `Authorization: MediaBrowser Token="…"`.

# Rehydrator v0.5.7 — Seerr webhook test support (unreleased)

## Fixed
- Accept authenticated Seerr test notifications without requiring a media ID or triggering rearming, allowing webhook settings to be saved.

# Rehydrator v0.5.6 — Jellyfin and durable Decypharr rearming (unreleased)

## Changed
- Jellyfin is the sole media-server integration. Remove the old media-server config/env settings and use `/api/playback/jellyfin` and `/api/jellyfin/*` endpoints. Targeted refreshes, independent refresh auditing, dedupe, metrics, dependency checks, and self-heal now use Jellyfin.
- Decypharr owns rearm, provider deletion, and status checks. TorBox credentials belong inside Decypharr; Rehydrator no longer calls TorBox directly.
- Routine rearm submits one explicitly typed source: original `.torrent` bytes or the original magnet. Infohashes remain identity only; mixed sources and implicit hash-to-magnet conversion are rejected. Missing sources are recovered from Arr history or explicitly flagged for recovery.
- Arr grab webhooks durably capture the original torrent file or resolved magnet before import; import notifications attach the exact download ID to media rows. Import sync remains a fallback. Metainfo is retained in PostgreSQL and uploaded using Decypharr's multipart `torrents` field.
- Rearm checks Decypharr completion plus mount visibility. If Arr has removed the completed queue entry, an exact retained-library hash and readable mounted file can establish readiness. Errors do not resubmit accepted downloads.
- Prune preserves source metadata first, requests provider deletion using Decypharr's browse API, and clears the old queue entry with `deleteFiles=false` to preserve local links. Provider removal is asynchronous inside Decypharr; acceptance does not independently confirm TorBox deletion.

## Added
- Migration `023_jellyfin_refresh_audit.sql` for independent Jellyfin refresh history; historical migration files and audit data are retained.
- Migration `022_explicit_torrent_source_kinds.sql` and database constraints for exclusive source payloads. Legacy files and extended magnets are classified; bare generated/ambiguous magnets and hash-only rows require source recovery. Newly captured genuine bare magnets remain valid.
- Migration `021_grab_source_capture.sql` for durable pre-import sources, retry state, and exact import associations, including season-pack episode files. Newer pending imports cannot reuse older release identities.
- Migration `020_saved_torrent_sources.sql` and matching automatic schema initialization for original torrent bytes, filenames, and hashes.
- Decypharr API-token authentication (`decypharr.api_token` / `DECYPHARR_API_TOKEN`).
- Infohash verification of stored/uploaded metainfo to reject mismatched files and invalid indexer responses.
- Jellyfin playback webhooks and automatic library notifications; setup in `docs/jellyfin-setup.md`.
- Regression coverage for exclusive source submission, magnet redirects, preserved links during prune, readiness checks, source integrity, and PostgreSQL byte-for-byte persistence.

## Fixed
- Prowlarr redirects to magnets are submitted to Decypharr without attempting to download the magnet over HTTP. Download fetch errors omit full URLs containing indexer credentials.
- Preserve full magnets, including trackers, when resolving Arr history.

## Upgrade notes
- Apply migrations 020–023 or enable `db_auto_migrate` before starting this revision. Enable On Grab and On Import (including upgrades) on both Arr webhook connections. Remove Completed can remain enabled.
- Set `REARM_BACKEND=decypharr` if using an earlier unreleased native-backend configuration. Provider pruning requires a Decypharr version exposing `DELETE /api/browse/torrents/{hash}` and an API token when authentication is enabled.
- Existing database rows cannot reconstruct a lost original torrent file from its hash alone. New imports capture their source while the original download URL is still accessible.
- The legacy `delete_files_on_prune` setting no longer controls provider pruning: local link deletion is always disabled.
- See `docs/decypharr-rearm.md` for limits and live validation.

---

# Build and release automation (unreleased)

## Added
- Optional Jellyfin integration with automatic path refreshes, authenticated playback webhooks, and movie/episode matching through Jellyfin item lookup. See `docs/jellyfin-setup.md`.
- Run Go tests and compile checks on pushes to `main` and pull requests targeting `main`.
- On version-tag pushes (`v*.*.*`), build Linux AMD64, Linux ARM64, and macOS ARM64 binaries and generate SHA-256 checksums.
- Publish multi-architecture (`linux/amd64`, `linux/arm64`) images to GHCR with versioned tags and `latest`.
- Automatically create the GitHub Release from a pushed version tag, or replace its binary assets on a workflow rerun if the release already exists.
- Scope write permissions to the publish job; normal commit and pull-request builds use read-only permissions.

## Fixed
- Handle Prowlarr download URLs that redirect to magnet URIs by submitting the resolved magnet to Decypharr, preserving its trackers. Keep torrent-file downloads working and omit full download URLs from fetch-error logs.

## Notes
- The Docker build defaults to version `dev`; release workflows pass the version parsed from the Git tag.
- Retrying a failed workflow for an existing tag should be done with GitHub Actions' **Re-run jobs**. Pushing an unchanged tag does not itself guarantee a new workflow run.

---

# Rehydrator v0.4.8 — Version reporting

## Added
- Print the embedded version during application startup, making the actual running build visible in pod logs.
- Add `--version` / `-version` CLI flag to print the version and exit before loading configuration or connecting to dependencies.
- Add Docker build argument `VERSION` and inject it into the binary using Go linker flags.
- Update the Makefile build target to embed the version from `VERSION`.

## Validation
- Go source formatted with `gofmt`.
- Full Go tests/build not run in this environment because dependency availability is not verified.

---

# Rehydrator v0.4.7 — Webhook diagnostics

## Added
- Structured `INFO` logs for authenticated Radarr and Sonarr webhook receipt, including event type, Arr ID when present, title when present, and remote address.
- Structured logs for Radarr/Sonarr refresh completion, refresh failures, debounce skips, and successful deletion reconciliation.
- `WARN` logs for webhook requests rejected due to unauthorized access, unsupported methods, or invalid JSON.
- Explicit `Test` event acknowledgement (`202 Accepted`, `action: test_acknowledged`) for both Arr webhook endpoints. Test events no longer trigger a library refresh.
- Processing outcome logs for Sonarr webhook events, including match status and whether refresh was performed or debounced.

## Security and behavior
- Logs intentionally exclude authorization headers, tokens, query strings, and complete webhook payloads.
- Existing deletion reconciliation, tombstones, and periodic inventory reconciliation behavior are retained from v0.4.6.

## Validation
- `gofmt` completed for the modified Go source.
- `go test ./internal/health` could not run because the checkout is missing required `go.sum` entries for `github.com/jackc/pgx/v5`; dependencies were not fetched in this environment. Tests are therefore **not verified**.

---

# Rehydrator v0.4.6 — Arr deletion reconciliation

## Added
- Immediate Radarr `MovieDelete` / `MovieDeleted` webhook handling.
- Immediate Sonarr `SeriesDelete` / `SeriesDeleted` webhook handling.
- Periodic full-inventory reconciliation for stale Radarr movie rows and Sonarr episode-file rows.
- `media_cache_tombstones` audit table retaining Arr source, IDs, title/external ID, reason, and deletion time.
- Tracking for Radarr movies present in Arr but not yet associated with an imported file, without queuing downloads.
- Migration `020_arr_deletion_reconciliation.sql` and webhook/reconciliation documentation.

## Safety
- Reconciliation runs only after successful full inventory retrieval.
- Sonarr pruning is skipped if any per-series episode or episode-file inventory request fails.
- Requested placeholders are retained.
- No media files or provider-side torrents are deleted by this feature.
- Re-adding an item to Arr allows normal sync to recreate active tracking; this change does not force a download.

## Validation note
`gofmt` completed. `go test ./...` could not run in the build environment because required Go modules were not cached and outbound DNS/network access to `proxy.golang.org` was unavailable. Run `go test ./...` in the normal build environment before deployment.
