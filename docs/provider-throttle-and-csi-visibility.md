# Provider scheduling and CSI visibility

`gateway.submit_interval_seconds` controls the shared per-tenant submission gap
(default 120, minimum 60 seconds). `gateway.active_poll_interval_seconds` controls
active torrent/NZB provider checks (default 30, range 15–3600 seconds). They are
independent of the 10-second dashboard database refresh and Arr client polling.

The worker handles bounded due jobs. Provider errors/missing placements use
five-minute checks, completed placements fifteen minutes, and placements awaiting
mount verification one minute. Capacity and provider latency may delay due work.

CSI-rclone directory caching can delay publication. The controller verifies actual
mounted bytes before announcing completion. It runs independently from the gateway,
so the gateway has no dependency on its own CSI volume. Never solve a mount delay
by reporting completion before mounted verification.

See [configuration](configuration.md) for current options and
[gateway setup](torbox-gateway-setup.md) for paths and deployments.
