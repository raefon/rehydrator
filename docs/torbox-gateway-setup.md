# Direct TorBox gateway (v0.7.7 playground)

Rehydrator accepts torrents and NZBs, submits them directly to TorBox, and exposes
one read-only WebDAV catalog for CSI-rclone. Decypharr is not used in this mode.
TorBox Pro is required for hosted Usenet. Prowlarr remains your indexer manager;
you do not supply an NNTP server to Rehydrator or TorBox's cloud downloader.

Use a fresh isolated deployment/database for the playground. Existing Decypharr
paths are not migrated. Nothing here requires deleting the working installation.
The gateway now serves a dashboard at `/`; see [dashboard and Authentik setup](../web/README.md).

## Split deployment with CSI

Use two processes for each tenant. The gateway can live in your dedicated
`rehydrator-gateways` namespace, and the controller stays in `tenet-nofear101`.
Use one gateway Deployment/Service and credential set per tenant; this is not a
single shared multi-tenant gateway. Match the tenant string, database, category
names, mount path and staging path across the pair.

| Role | Responsibilities | Mounts |
|---|---|---|
| `gateway` | TorBox submissions/polling, qBittorrent/SAB, WebDAV | Secret/config only; no tenant PVCs |
| `controller` | Staging links, mounted-file verification, Arr/Seerr/Jellyfin | CSI plus shared media/config as needed |
| `combined` | Existing single-process behavior (default) | Suitable only where the mount does not depend on this pod starting |

Set YAML `role`, environment `REHYDRATOR_ROLE`, or `--role`; CLI takes precedence,
then environment, then YAML. Split roles require `gateway.enabled: true` and
`prune_enabled: false`; the older cloud-only mode remains combined-only.

Start with [gateway config](../config/rehydrator.gateway.example.yaml) and
[controller config](../config/rehydrator.controller.example.yaml). The controller
does not require TorBox, qBittorrent, SAB or DAV secrets; it coordinates through
the same PostgreSQL database. Only it needs Arr/Jellyfin credentials and the
usual API token for incoming webhooks. Mount paths in the gateway config are
logical consumer paths, not volumes that should be mounted in that pod.

~~~sh
rehydrator --role=gateway --config=/etc/rehydrator/config.yaml
rehydrator --role=controller --config=/config/rehydrator.yaml
~~~

The gateway publishes ready provider catalogs as `Mounting`. The controller
independently leases verification work, creates staging links and performs the
bounded mounted-file read before marking `Completed`. Provider polling and
verification have separate due times; catalog revisions fence stale completion
after provider loss/replacement. With the controller stopped, DAV metadata remains
available and new downloads wait in `Mounting` instead of prematurely completing.
Gateway delete-data requests enqueue durable staging cleanup for the controller.

Gateway `/readyz` checks database/catalog access without asking the controller,
Arr or the CSI mount. `/healthz` is process liveness. Apply migrations through 033
before rollout, or let the gateway initialize the schema first. Do not run
schema repair concurrently with active controllers. The gateway must not wait
for `/config/.permissions-initialized` or depend on a pod that needs its CSI mount.

Point CSI and download clients to:

~~~text
http://tenet-nofear101-gateway.rehydrator-gateways.svc.cluster.local:8080/webdav/
~~~

Use that host with an empty URL Base for qBittorrent or `/sabnzbd` for SAB.
Keep Arr/Seerr/Jellyfin webhooks pointed at the tenant **controller** Service.
The gateway role exposes no controller/admin routes. Ensure existing NetworkPolicies
permit tenant and CSI-node traffic to the gateway and both roles to PostgreSQL.
A runnable-shape Kubernetes example is in
[deploy/gateway-controller.example.yaml](../deploy/gateway-controller.example.yaml);
create its referenced configuration Secrets and replace image/claim names first.
No live cluster resources are changed by these examples.

## Combined configuration

For split CSI deployments use the role-specific examples above. The following
configuration describes the original combined mode.

Add this to your existing Rehydrator config, supplying credentials through your
Secret/config renderer or the corresponding environment variables:

```yaml
prune_enabled: false
rearm_enabled: true
rearm_backend: torbox
cloud_usenet:
  enabled: false
gateway:
  enabled: true
  api_key: ""            # GATEWAY_API_KEY: TorBox key, only Rehydrator receives it
  qb_username: arr        # GATEWAY_QB_USERNAME
  qb_password: ""        # GATEWAY_QB_PASSWORD
  sab_api_key: ""        # GATEWAY_SAB_API_KEY: separate client secret
  dav_username: rclone    # GATEWAY_DAV_USERNAME
  dav_password: ""       # GATEWAY_DAV_PASSWORD: separate mount secret
  mount_path: /storage/torbox
  download_path: /storage/media/downloads/rehydrator
  submit_interval_seconds: 120
  recover_missing: true
```

The API server must be enabled (normally port 8080). Keep its usual API token and
existing Arr/Jellyfin/Seerr configuration. Gateway credentials are separate from
that token. `gateway` and `cloud_usenet` cannot both be enabled. Automatic provider
pruning is deliberately unavailable in direct mode; startup rejects
`prune_enabled: true`.

`db_auto_migrate` applies the embedded schema. If you manage migrations manually,
apply through `033_gateway_roles.sql`. It adds independent retained sources,
shared jobs and hashed expiring client sessions. Existing cloud-NZB rows are
copied without changing their job IDs; the legacy table is retained.

Direct mode uses the shared gateway worker instead of the legacy filesystem
file-health worker. It polls downloading jobs about once per minute and completed
jobs about every 15 minutes, processing at most five due jobs per 15-second pass.
Large libraries therefore take multiple passes. It checks provider placement
metadata without reading every media file. Initial completion and recovered
placement completion require a bounded mounted-file read.

## CSI mount

Configure your CSI rclone remote using [torbox-gateway-rclone.example.conf](../config/torbox-gateway-rclone.example.conf):

```ini
[rehydrator]
type = webdav
url = http://REHYDRATOR:8080/webdav/
vendor = other
user = rclone
pass = RCLONE_OBSCURED_DAV_PASSWORD
```

Generate the password value with `rclone obscure` and keep the config in a Secret.
Mount the remote root at `/storage/torbox` in the Rehydrator controller, Sonarr, Radarr and
Jellyfin. Share `/storage/media` with those pods too. Rehydrator serves HTTP;
CSI-rclone owns FUSE. It does not mount anything itself.

The stable tree is:

```text
/storage/torbox/torrents/torrent-<uuid>/<provider-relative-file>
/storage/torbox/usenet/cloud-nzb-<uuid>/<provider-relative-file>
```

Stage links under `/storage/media/downloads/rehydrator/<category>/<job-id>/`.
Paths remain stable when a provider ID changes. Imported library symlinks must
retain the gateway target, not resolve to an old Decypharr path. Choose symlink
imports for your storage layout; do not copy entire remote media into local
storage accidentally. Keep mount/download roots distinct and outside each other.
An unreachable gateway can leave an existing mount serving stale directory data;
validate actual bounded reads from each consumer after remounting.

## Sonarr and Radarr

Add a **qBittorrent** download client for torrents:

- Host: your Rehydrator gateway Service; port: 8080; URL Base: empty.
- Username/password: gateway `qb_username` / `qb_password`.
- Category: `sonarr` or `radarr` (or the configured `decypharr.*_category` values).
- Initial state: Start. Content layout: Original.
- Disable sequential download, first/last-piece priority, force-start and tags.
- Leave per-release seeding/ratio overrides unset. TorBox manages seeding.

For the complete hosted-NZB test procedure, see [NZB gateway setup](nzb-gateway-setup.md).

Add a **SABnzbd** client for Usenet:

- Same host and port; URL Base: `/sabnzbd`.
- API key: gateway `sab_api_key`; category: `sonarr` or `radarr`.
- Prowlarr still supplies your NZB indexers. Upload the original NZB through Arr.

Keep Arr grab/import webhooks and inventory sync enabled. The gateway stores the
original source before acknowledging the add. Arr source capture reads that
local job instead of fetching the release again from Prowlarr. Inventory sync
can recover the association from an imported gateway symlink after Arr removes
its completed queue history. Both movies and TV episodes use this flow.

The qBittorrent compatibility API implements the Arr download-client subset,
not a complete torrent daemon. HTTP torrent URL downloads are rejected: submit
an original `.torrent` file or a v1 magnet. V2/hybrid metainfo is rejected. Pause,
force-start, provider seeding changes and other unsupported controls return
errors. `topPrio` returns 409 because provider queue priority is not controlled
here. Configure the client accordingly.

Queue deletion hides client history. qBittorrent `deleteFiles=true` removes only
known staging symlinks; it never deletes imported files, retained sources,
WebDAV catalog entries or TorBox payloads. SAB history deletion hides the job.
This allows completed-download cleanup without losing future recovery data.

## Recovery and inspection

Three fresh missing-placement checks, spaced at least five minutes apart, queue
the retained original source when recovery and `rearm_enabled` are enabled. A
shared durable reservation prevents concurrent uploads; each job allows at most
three total submissions. The 120-second submission gap is shared by torrent and
NZB jobs for this tenant. Jobs are tenant-scoped; use one gateway owner per TorBox
account to avoid independent instances competing for its limits.

Provider/API errors do not prove eviction. An indeterminate add or interrupted
submission becomes `SubmissionUnknown` and stops for inspection. A stale torrent
still listed in TorBox with no usable placement also needs provider inspection;
Rehydrator does not silently delete it. Changed file names/sizes block automatic
recovery instead of redirecting imported links to different content. Mount-only
failures wait for mount visibility instead of uploading again.

Related imported items enter visibility waiting during recovery. After the
provider is ready and the imported path is readable, the existing controller
marks it available and queues the configured Jellyfin refresh. Playback is not
required to trigger provider-loss recovery.

Log in with the qBittorrent credentials to inspect redacted jobs:

```sh
curl -c /tmp/rehydrator-cookies \
  --data-urlencode 'username=arr' --data-urlencode "password=$GATEWAY_QB_PASSWORD" \
  http://REHYDRATOR:8080/api/v2/auth/login
curl -b /tmp/rehydrator-cookies http://REHYDRATOR:8080/api/v2/gateway/jobs
curl -b /tmp/rehydrator-cookies -X POST --data-urlencode 'id=torrent-UUID' \
  http://REHYDRATOR:8080/api/v2/gateway/reconcile
```

Reconcile advances the next observation; it does not reset retry limits, erase
unknown submission outcomes or force another add. Remove the cookie file after
use. Sessions are hashed in PostgreSQL and expire after 12 hours.

## Playground acceptance

Automated tests exercise PostgreSQL reservations, restart behavior, original
source retention, qBittorrent/SAB request shapes, metadata-only DAV, range proxy,
provider-loss recovery and legacy routing. They use provider mocks and simulated
mounted files, not a live TorBox account or CSI cluster.

Before relying on this deployment, test one magnet, one original torrent file and
one hosted NZB end-to-end in each applicable Arr. Verify the client Test, actual
import, queue removal, Jellyfin playback and seek, service restart and controlled
provider-loss recovery with Decypharr stopped. Confirm reads in every consumer
pod and watch API errors before increasing submission rates. These live checks
remain deployment work; no cluster configuration is changed by this code.

## Recovering queue-only submissions from v0.7.1

Version 0.7.1 accidentally requested `as_queued=true` for provider submissions.
Version 0.7.2 requests normal scheduling for both torrent and NZB adds. It does not
start previously queued provider items automatically. Start the existing torrent
in TorBox rather than submitting another copy. A `SubmissionUnknown` torrent job
is then checked against account inventory by exact hash about every five
minutes and can adopt the active torrent ID. Version 0.7.3 accepts an exact
positive identity match without requesting further pages. An absent hash still
requires complete inventory; repeated/partial pages stay inconclusive. This is observation-only: absent,
queued-only or inconclusive results retain the unknown state and never authorize
a duplicate upload. Queue IDs are not active torrent IDs. Automatic unknown-outcome
reconciliation for NZBs remains unavailable because they have no torrent hash.

## Sonarr base-directory import warning

Versions before 0.7.4 reported identical qBittorrent `save_path` and `content_path`,
which Sonarr refuses to import. Version 0.7.4 reports:

~~~text
save_path:    /storage/media/downloads/rehydrator/sonarr
content_path: /storage/media/downloads/rehydrator/sonarr/torrent-UUID
files.name:   torrent-UUID/Release/Episode.mkv
~~~

Files remain staged in their existing locations. Update the gateway image and
let Sonarr poll the queue again; no provider re-add, file move or source reset
is required. Keep Original content layout. If a path remains inaccessible after
that, verify Sonarr sees the same shared staging volume as the controller.
