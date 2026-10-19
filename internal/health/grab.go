package health

import (
	"net/http"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

func grabFromWebhook(tenant, arr string, payload map[string]any) model.GrabSource {
	owner := firstMap(payload, "movie", "Movie")
	if arr == "sonarr" {
		owner = firstMap(payload, "series", "Series")
	}
	release := firstMap(payload, "release", "Release")
	return model.GrabSource{Tenant: tenant, Arr: arr, OwnerID: firstInt(owner, "id"), DownloadID: firstString(payload, "downloadId", "downloadID"), Torrent: model.TorrentMetadata{
		Source: arr, SourceTitle: firstString(release, "releaseTitle", "title"), TorrentURL: firstString(release, "downloadUrl", "downloadURL"), Magnet: firstString(release, "magnetUrl", "magnet"), InfoHash: firstString(release, "infoHash", "torrentInfoHash"),
	}}
}

func (h *apiHandler) handleGrabCapture(w http.ResponseWriter, r *http.Request, arr string, payload map[string]any) {
	g := grabFromWebhook(h.tenant, arr, payload)
	if g.OwnerID <= 0 || strings.TrimSpace(g.DownloadID) == "" {
		http.Error(w, "grab webhook requires owner ID and downloadId", http.StatusBadRequest)
		return
	}
	if g.Torrent.Magnet != "" && g.Torrent.TorrentURL != "" {
		http.Error(w, "grab must specify one torrent source", http.StatusBadRequest)
		return
	}

	if h.captureGrab == nil {
		http.Error(w, "grab source capture is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := h.captureGrab(r.Context(), g); err != nil {
		http.Error(w, "grab source capture failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "action": "grab_source_queued", "arr": arr, "owner_id": g.OwnerID})
}
