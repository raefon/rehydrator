# Configuration

Rehydrator uses the direct TorBox gateway for torrents and hosted NZBs. TorBox Pro
is required for hosted NZBs; no NNTP provider credentials belong in Rehydrator.

Use these examples:

- [Gateway](../config/rehydrator.gateway.example.yaml): provider credentials, download-client endpoints, dashboard and WebDAV. Mount only its configuration Secret.
- [Controller](../config/rehydrator.controller.example.yaml): Arr/Seerr/Jellyfin integrations, CSI and shared media mounts. Uses the same database and tenant as the gateway.
- [Combined](../config/rehydrator.example.yaml): local/testing deployments where mounting WebDAV does not depend on this process starting.

Configure categories under `gateway` in both processes:

```yaml
gateway:
  enabled: true
  radarr_category: radarr
  sonarr_category: sonarr
  active_poll_interval_seconds: 30
  submit_interval_seconds: 120
```

The environment overrides are `GATEWAY_RADARR_CATEGORY`, `GATEWAY_SONARR_CATEGORY`,
`GATEWAY_ACTIVE_POLL_INTERVAL_SECONDS`, and `GATEWAY_SUBMIT_INTERVAL_SECONDS`.
See [.env.example](../.env.example) for the supported overrides. CLI `--role`
overrides `REHYDRATOR_ROLE`, which overrides YAML. All configs are strict: unknown
or removed YAML fields fail startup instead of being silently ignored.

## Upgrade from earlier configurations

Remove `decypharr`, `cloud_usenet`, `torbox_health`, `rearm_backend`, all prune
settings, `rearm_short_circuit_if_csi_visible`, `csi_wait_seconds`, and
`provider_cooldown_seconds`. Remove the corresponding environment variables too.
Move category values from the removed Decypharr block to `gateway.radarr_category`
and `gateway.sonarr_category`. Remove `jellyfin.refresh_after_prune`.

The old periodic filesystem/provider-health sweep is superseded by gateway
placement polling and retained-source recovery. Under `file_health`, keep only
`canary_path` if you use Jellyfin inventory's `mark_missing_in_arr` option. That
same-mount read safeguard remains enforced before changing Arr file records.

The old `/sabnzbd-cloud/api`, `/cloud-usenet/dav/`, and `/api/prune/*` routes are
removed. Use `/sabnzbd/api` for hosted NZBs, `/api/v2/` for torrents, and `/webdav/`
for the shared catalog. Automatic provider pruning is not supported.

This upgrade does not delete PostgreSQL records, retained source payloads,
provider placements, or media links. Historical SQL migrations and table names
remain for upgrade compatibility, and existing `cloud-nzb-…` job IDs stay stable.
Old library links pointing to another WebDAV layout are not automatically migrated.
Use a database backup and reconcile those paths deliberately.

Apply schema through 035 before new binaries, or start the gateway with
`db_auto_migrate: true` before upgrading the controller. No cleanup migration or
manual table drop is necessary for this consolidation.

For native Authentik OIDC and ForwardAuth, see [dashboard setup](../web/README.md).

## Separate 4K controllers

Use `library_edition: 4k` only on a dedicated controller, retaining the real
`tenant` used by the shared gateway. Set absolute `library_paths` for that
controller's roots and `gateway.additional_categories` for the other edition's
categories. `seerr_4k_controller_url` on the standard controller forwards verified
4K Seerr requests to the second controller. See [separate 4K setup](separate-4k.md).
