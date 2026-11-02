# Rehydrator

Rehydrator sends torrents and hosted NZBs directly to TorBox, retains their original
sources in PostgreSQL, and exposes a shared WebDAV catalog for CSI-rclone. A library
controller stages and verifies files for Sonarr/Radarr and refreshes Jellyfin.
Seerr and playback webhooks can request restoration of tracked releases.

Current version: **v0.8.4**. Run `rehydrator --version` to inspect your binary.

## Deployment

| Role | Responsibilities | Mounts |
|---|---|---|
| Gateway | TorBox submission/polling, qBittorrent/SAB endpoints, WebDAV, dashboard | Configuration Secret only |
| Controller | Staging, bounded mounted-file verification, Arr/Seerr/Jellyfin integration | Shared media and CSI mount |
| Combined | Both roles for local testing | Suitable when mount startup does not depend on this process |

For Kubernetes CSI, run separate gateway/controller processes per tenant. Both
use the same PostgreSQL database, tenant, categories and logical paths. The gateway
must start independently of its CSI consumers.

- [Gateway/controller setup](docs/torbox-gateway-setup.md)
- [Configuration and upgrade changes](docs/configuration.md)
- [Hosted-NZB setup](docs/nzb-gateway-setup.md) — requires TorBox Pro
- [Dashboard, Authentik OIDC and ForwardAuth](web/README.md)
- [Jellyfin setup](docs/jellyfin-setup.md)
- [Seerr and Arr webhooks](docs/radarr-seerr-setup.md)

## Workflow

1. Arr submits a magnet/torrent via the gateway's qBittorrent API or an NZB via SAB.
2. The exact original source is saved before the submission is acknowledged.
3. TorBox downloads/processes the release. The gateway records provider observations.
4. Ready files appear through WebDAV; the controller stages links and verifies a bounded read.
5. The client reports completion for Arr to import, then Jellyfin is refreshed.
6. Confirmed missing placements use the retained source through bounded recovery.

Provider progress is distinct from library readiness: download 100% may still mean
processing. A torrent file and magnet are separate submission forms; hashes are
not automatically converted into magnets. Unknown submissions never authorize a
blind duplicate upload. Automatic provider pruning is not supported.

## Dashboard

The gateway serves `/`: download activity and retained history, file metadata,
provider progress, source/identity diagnostics, observation-only checks and guarded
recovery requests. Use native Authentik OIDC, explicit ForwardAuth delegation or
the gateway qBittorrent credentials for dashboard authentication. Provider keys
and original source contents are not sent to the browser.

## Build and verification

```sh
make release-build  # Node.js 22.12+ and Go 1.25; builds and embeds frontend assets
make version
```

Plain `make build` uses the checked-in frontend bundle. The Dockerfile rebuilds
frontend assets in its build stage; Node is not part of the runtime image.
Tests that need PostgreSQL use `REHYDRATOR_TEST_POSTGRES_URL` pointing to a disposable
database. Run `go test -race -p 1 ./...` and `go vet ./...`.

## Upgrading

The v0.8.0 consolidation removes Decypharr, the separate cloud-NZB backend, old prune
routes and the superseded file/provider sweep. Update your YAML and environment
using [the migration guide](docs/configuration.md#upgrade-from-earlier-configurations).
Unknown/removed YAML fields now fail startup. Existing sources, job IDs, database
records, schema upgrades and WebDAV paths are preserved; no database wipe is needed.
