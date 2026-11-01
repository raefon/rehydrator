# Configuration examples

- `rehydrator.gateway.example.yaml`: provider gateway; Secret mount only.
- `rehydrator.controller.example.yaml`: library controller; CSI and shared media mounts.
- `rehydrator.example.yaml`: combined local/testing configuration.
- `torbox-gateway-rclone.example.conf`: shared CSI-rclone WebDAV remote.

Examples contain placeholders, not live credentials. Supply secrets through your
Secret/config renderer or environment overrides. Gateway and controller must use
the same tenant, database, categories, and logical paths.

See [configuration and upgrades](../docs/configuration.md). Do not mount the CSI
volume into the gateway: its WebDAV endpoint must start before consumers can mount.
