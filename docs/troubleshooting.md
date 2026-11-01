# Troubleshooting

Start with the gateway dashboard's Recovery & diagnostics page. Look up the exact
torrent hash or gateway job ID to inspect retained-source validity, provider ID,
workflow state, next checks and linked library rows.

- **Queued:** waiting for the shared submission gate or worker capacity.
- **SubmissionUnknown:** inspect TorBox; do not reset attempts or blindly resubmit.
- **TorBox processing at 100%:** transfer completion is not provider readiness.
- **Mounting:** check controller logs, shared mount visibility and staging permissions.
- **Completed but not imported:** check the Arr queue warning and client polling task.
- **Missing:** scheduled confirmations precede retained-source recovery.
- **Failed:** inspect the failure; acquire a replacement through Arr when recovery is blocked.
- **No saved source:** regrab through Arr or upload the original torrent/NZB/magnet.

Hidden Arr history is retained in the dashboard. Downloads added directly to TorBox
before this gateway aren't automatically gateway history. Old symlinks using a
different remote layout do not become valid just by changing the WebDAV URL.

If startup rejects config, compare against the current role-specific examples and
remove retired keys/environment variables; see [configuration](configuration.md).
No database wipe is required. Upgrade the schema through migration 034 before new
processes start.
