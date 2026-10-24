package health

import "net/http"

// Stored observations include timestamps; this endpoint does not call TorBox.
func (h *apiHandler) handleTorBoxHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	items, err := h.repo.ProviderHealthReport(r.Context(), h.tenant, 200)
	if err != nil {
		http.Error(w, "could not load provider observations", 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": "torbox", "scope": "tracked_torrent_releases", "items": items})
}
