# TorBox gateway and controller

Use one gateway/controller pair per tenant, with a shared PostgreSQL database and
matching tenant/category/path settings. TorBox Pro is required for hosted NZBs.
Prowlarr remains the indexer manager; Rehydrator does not need NNTP credentials.

## Split deployment with CSI

Start with [gateway config](../config/rehydrator.gateway.example.yaml) and
[controller config](../config/rehydrator.controller.example.yaml). Both processes
serve port 8080 by default on their separate Services.

- **Gateway:** configuration Secret only. Do not mount CSI, the library PVC, or a
  tenant permission-marker init container. It exposes `/api/v2/`, `/sabnzbd/api`,
  `/webdav/`, and the dashboard at `/`.
- **Controller:** mount CSI at `/storage/torbox` and writable shared media at
  `/storage/media`. It exposes the authenticated controller/webhook APIs and needs
  Arr/Jellyfin/Seerr credentials, not provider/client credentials.
- **Combined:** available for local setups whose mount does not depend on the
  gateway pod starting. It uses the same direct TorBox backend, not a legacy mode.

```sh
rehydrator --role=gateway --config=/etc/rehydrator/config.yaml
rehydrator --role=controller --config=/config/rehydrator.yaml
```

Apply schema through 035 before rollout, or start the gateway with
`db_auto_migrate: true` before controllers. Use false on controllers. `/healthz`
checks process liveness; gateway `/readyz` checks the database/catalog without
requiring Arr or CSI. The [deployment example](../deploy/gateway-controller.example.yaml)
shows the shape; replace image, Secret, Service and claim names.

## Download clients and shared mount

Configure Arr qBittorrent with the gateway host, port 8080, empty URL Base, and
`gateway.qb_username` / `gateway.qb_password`. Use category `sonarr` or `radarr`,
matching `gateway.sonarr_category` / `gateway.radarr_category` in both processes.
Configure hosted NZBs using [the SAB setup](nzb-gateway-setup.md).

Point CSI-rclone's WebDAV remote to the gateway Service:

```ini
[torbox]
type = webdav
url = http://tenet-rehydrator-gateway.tenet-nofear101.svc.cluster.local:8080/webdav/
vendor = other
user = rclone
pass = YOUR_RCLONE_OBSCURED_DAV_PASSWORD
```

The gateway configuration uses the plain `dav_password`. Rclone's `pass` uses its
obscured form; seal the resulting Secret through your normal deployment tooling.
Keep the CSI remote name/path consistent with your PV. Consumers share the CSI
claim; the gateway itself never mounts it.

Stable paths are `/storage/torbox/torrents/torrent-…/…` and
`/storage/torbox/usenet/cloud-nzb-…/…`. Staging paths are
`/storage/media/downloads/rehydrator/<category>/<job-id>/…`. Create category staging
directories on the shared media volume and keep paths identical in Arr/controller.
Old links pointing to another layout are not automatically translated.

## Completion and recovery

The gateway saves one exact magnet, torrent file or NZB before acknowledging a
client. Sources are retained independently of library rows and queue history.
Provider-ready catalogs are published as `Mounting`; a separately leased controller
creates staging links and reads a bounded sample before marking `Completed`.
Catalog revisions prevent stale controller work from completing a replacement.
With the controller stopped, jobs remain Mounting while WebDAV metadata stays usable.

The worker processes at most five due provider jobs per pass. Active checks default
to 30 seconds, completed jobs 15 minutes, mount-wait provider checks one minute,
and failures/missing placements five minutes. Submission spacing defaults to 120
seconds. See [polling configuration](../web/README.md#active-polling-interval).

Three missing observations can queue the original source if `gateway.recover_missing`
and `rearm_enabled` allow recovery. A job has at most three submission attempts.
Unknown outcomes are observation-only: exact torrent hash reconciliation may adopt
an existing active placement, but does not authorize another upload. Unknown NZBs
require provider inspection. Mount failures do not trigger provider reuploads.

Arr may remove completed client history normally; hidden jobs remain in the catalog,
dashboard and retained-source database. Staging cleanup does not delete original
sources or imported library links. Automatic provider pruning is unavailable.

## Dashboard and acceptance

See [dashboard/SSO setup](../web/README.md). The gateway supports a live queue,
file catalog, provider observations and Recovery & diagnostics. These views read
PostgreSQL; only the explicit provider-check action makes an additional lookup.

Test a single torrent and NZB before broad acquisition. Confirm durable source
storage, provider completion, WebDAV visibility, controller verification, Arr
import and Jellyfin refresh. Unit tests use provider mocks and simulated mounted
files; they do not replace a live CSI/provider test.
