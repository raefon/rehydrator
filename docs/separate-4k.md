# Separate 1080p and 4K libraries

Run one gateway and two controller deployments per real tenant. Both controllers
use the same `tenant`, PostgreSQL database, API token and CSI mount. The 4K
controller sets `library_edition: 4k`; it stores library and Seerr state under
`<tenant>:4k`. Standard state stays under `<tenant>`, preserving existing IDs.
This isolates overlapping Sonarr/Radarr IDs without duplicating provider jobs or
changing the WebDAV paths. Do not set the gateway's tenant to the suffixed scope.

The standard controller connects to the standard Arr pair, and the 4K controller
connects to the second pair. Give all gateway/controller processes all four
categories through `gateway.additional_categories`. The gateway accepts normal
`sonarr`/`radarr` plus `sonarr-4k`/`radarr-4k`; the 4K controller uses the latter as
its primary categories and lists the standard pair as additional categories.

Use independent root paths: `/storage/media/movies`, `/storage/media/series`,
`/storage/media/movies-4k` and `/storage/media/series-4k`. Set `library_paths` on each
controller to its own roots. Deliver Jellyfin playback webhooks to both; each
controller ignores paths outside its own roots. Library records, source recovery,
refresh deduplication and Arr API operations remain edition-scoped.

Seerr has one non-4K default and one 4K default for each Arr service type. Polling
filters requests by `is4k`. Because webhook templates may omit that flag, the
controller looks up the request ID through Seerr. The standard controller can set
`seerr_4k_controller_url` to the internal 4K controller URL to forward verified 4K
webhooks immediately using the shared API token. Failed verification does not
infer an edition; scheduled request reconciliation remains available.

See [standard controller](../config/rehydrator.controller.example.yaml) and
[4K controller](../config/rehydrator.controller-4k.example.yaml) examples. Use
separate API keys/config directories/databases for the second Arr pair. Existing
tenants do not become 4K-enabled merely by upgrading the Rehydrator binary.
The companion kdlhost-cluster provisioning and kdlhost-charts tenant chart automate
these pieces for newly provisioned tenants. Publish the Rehydrator 0.9.0 image and
tenant chart 0.4.29 before applying those GitOps changes.
