# Sonarr episode support

Configure Sonarr with the gateway qBittorrent client for torrents and SABnzbd
client for hosted NZBs. Use category `sonarr`, or match `gateway.sonarr_category`
in both gateway/controller configurations. See [gateway setup](torbox-gateway-setup.md)
and [NZB setup](nzb-gateway-setup.md).

Configure On Grab and On Import webhooks to the controller, and enable the Sonarr
seed sync to inventory existing episode files. Each tracked library row uses the
Sonarr episode-file ID, not the series ID. Episodes in a season pack share their
provider placement and retained gateway source; provider recovery is performed
per gateway job rather than uploading once per episode.

Keep the shared mount and staging paths identical in Arr and the controller.
Playback and Seerr requests can queue library restoration, but an unavailable
original source requires acquiring a new release through Arr. A torrent hash alone
is not converted into a magnet. Jellyfin refreshes follow verified visibility.
