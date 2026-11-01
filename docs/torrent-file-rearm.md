# Retained torrent sources

The gateway accepts exactly one magnet or `.torrent` file per submission. Original
torrent bytes are retained before acknowledgement; they are never reconstructed
from an infohash. NZB files use their own protocol and stable job identity.

Rearm and confirmed provider-loss recovery use the retained original source, not
an expiring Arr/Prowlarr download URL. A hash without a valid retained source is
insufficient: acquire a replacement through Arr or submit the original source.
The gateway currently supports v1 torrent metainfo; unsupported v2/hybrid sources
are rejected rather than silently converted.

The dashboard Recovery & diagnostics page shows source kind, validity, linked
library entries and recovery eligibility without exposing original bytes, tracker
credentials or provider keys. See [gateway setup](torbox-gateway-setup.md).
