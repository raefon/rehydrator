package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
)

type Server struct {
	addr string
	srv  *http.Server
}

type APIOptions struct {
	Addr           string
	Repo           *db.Repo
	Tenant         string
	Token          string
	RequireToken   bool
	MetricsEnabled bool
	RefreshRadarr  func(context.Context) error
	RefreshSeerr   func(context.Context) error
}

func NewServer(addr string) *Server {
	return newServer(APIOptions{Addr: addr})
}

func NewAPIServer(opt APIOptions) *Server {
	return newServer(opt)
}

func newServer(opt APIOptions) *Server {
	addr := opt.Addr
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	if opt.Repo != nil {
		api := &apiHandler{
			repo:           opt.Repo,
			tenant:         opt.Tenant,
			token:          opt.Token,
			requireToken:   opt.RequireToken,
			metricsEnabled: opt.MetricsEnabled,
			refreshRadarr:  opt.RefreshRadarr,
			refreshSeerr:   opt.RefreshSeerr,
		}
		mux.HandleFunc("/metrics", api.handleMetrics)
		mux.HandleFunc("/api/state/movie/", api.handleStateMovie)
		mux.HandleFunc("/api/prune/movie/", api.handlePruneMovie)
		mux.HandleFunc("/api/rearm/movie/tmdb/", api.handleRearmMovieTMDB)
		mux.HandleFunc("/api/rearm/movie/", api.handleRearmMovie)
		mux.HandleFunc("/api/refresh/radarr", api.handleRefreshRadarr)
		mux.HandleFunc("/api/refresh/seerr", api.handleRefreshSeerr)
		mux.HandleFunc("/api/seerr/webhook", api.handleSeerrWebhook)
		mux.HandleFunc("/api/seerr/rearm", api.handleSeerrRearm)
		mux.HandleFunc("/api/state", api.handleState)
	}

	return &Server{
		addr: addr,
		srv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

func (s *Server) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("health/api server shutdown failed", "error", err)
		}
	}()

	slog.Info("health/api server starting", "addr", s.addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("health/api server failed", "error", err)
	}
}

type apiHandler struct {
	repo           *db.Repo
	tenant         string
	token          string
	requireToken   bool
	metricsEnabled bool
	refreshRadarr  func(context.Context) error
	refreshSeerr   func(context.Context) error
}

func (h *apiHandler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.metricsEnabled {
		http.NotFound(w, r)
		return
	}

	snap, err := h.repo.Metrics(r.Context(), h.tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "rehydrator_rearm_requested_items{tenant=%q} %d\n", h.tenant, snap.RearmRequested)
	_, _ = fmt.Fprintf(w, "rehydrator_failed_items{tenant=%q} %d\n", h.tenant, snap.FailedItems)
	_, _ = fmt.Fprintf(w, "rehydrator_expired_prune_items{tenant=%q} %d\n", h.tenant, snap.ExpiredPruneQueued)
	_, _ = fmt.Fprintf(w, "rehydrator_seerr_requests_total{tenant=%q} %d\n", h.tenant, snap.SeerrRequests)
	_, _ = fmt.Fprintf(w, "rehydrator_seerr_rearmed_total{tenant=%q} %d\n", h.tenant, snap.SeerrRearmed)
	_, _ = fmt.Fprintf(w, "rehydrator_events_total{tenant=%q} %d\n", h.tenant, snap.EventsTotal)
	states := make([]string, 0, len(snap.ItemsByState))
	for state := range snap.ItemsByState {
		states = append(states, state)
	}
	sort.Strings(states)
	for _, state := range states {
		_, _ = fmt.Fprintf(w, "rehydrator_items_by_state{tenant=%q,state=%q} %d\n", h.tenant, state, snap.ItemsByState[state])
	}
}

func (h *apiHandler) handleRearmMovie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}

	arrID, ok := idFromPath(w, r.URL.Path, "/api/rearm/movie/")
	if !ok {
		return
	}
	item, err := h.repo.RequestRearm(r.Context(), h.tenant, model.MediaMovie, arrID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusAccepted, rearmResponse(item, "manual_arr_id"))
}

func (h *apiHandler) handleRearmMovieTMDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}

	tmdbID, ok := idFromPath(w, r.URL.Path, "/api/rearm/movie/tmdb/")
	if !ok {
		return
	}
	item, matched, err := h.repo.RequestRearmByTMDB(r.Context(), h.tenant, model.MediaMovie, tmdbID, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !matched {
		http.Error(w, "no tracked movie row matched tmdb id", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusAccepted, rearmResponse(item, "manual_tmdb_id"))
}

func (h *apiHandler) handlePruneMovie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	arrID, ok := idFromPath(w, r.URL.Path, "/api/prune/movie/")
	if !ok {
		return
	}
	if r.URL.Query().Get("dry_run") == "true" {
		item, found, err := h.repo.GetState(r.Context(), h.tenant, model.MediaMovie, arrID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "movie row not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "would_prune": item.State == model.StateAvailable, "item": item})
		return
	}
	item, err := h.repo.RequestPrune(r.Context(), h.tenant, model.MediaMovie, arrID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "source": "manual_prune", "item": item})
}

func (h *apiHandler) handleRefreshRadarr(w http.ResponseWriter, r *http.Request) {
	h.handleRefresh(w, r, "radarr", h.refreshRadarr)
}

func (h *apiHandler) handleRefreshSeerr(w http.ResponseWriter, r *http.Request) {
	h.handleRefresh(w, r, "seerr", h.refreshSeerr)
}

func (h *apiHandler) handleRefresh(w http.ResponseWriter, r *http.Request, name string, fn func(context.Context) error) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	if fn == nil {
		http.Error(w, name+" refresh is not configured", http.StatusNotFound)
		return
	}
	if err := fn(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "refresh": name})
}

func (h *apiHandler) handleSeerrWebhook(w http.ResponseWriter, r *http.Request) {
	h.handleSeerrPost(w, r, false)
}
func (h *apiHandler) handleSeerrRearm(w http.ResponseWriter, r *http.Request) {
	h.handleSeerrPost(w, r, true)
}

func (h *apiHandler) handleSeerrPost(w http.ResponseWriter, r *http.Request, forceDefault bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}

	defer r.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	mediaType := normalizeMediaType(firstString(payload, "mediaType", "type"))
	media := firstMap(payload, "media", "mediaInfo")
	if mediaType == "" {
		mediaType = normalizeMediaType(firstString(media, "mediaType", "type"))
	}
	if mediaType == "" {
		mediaType = model.MediaMovie
	}
	tmdbID := firstInt(payload, "tmdbId", "tmdbID")
	if tmdbID == 0 {
		tmdbID = firstInt(media, "tmdbId", "tmdbID")
	}
	arrID := firstInt(payload, "arrId", "radarrId", "movieId")
	requestID := firstInt(payload, "requestId", "id")
	title := firstString(media, "title", "name")
	if title == "" {
		title = firstString(payload, "title", "subject", "mediaName")
	}
	status := firstString(payload, "status", "event", "notification_type", "notificationType")
	force := forceDefault
	if v, ok := payload["force"].(bool); ok {
		force = v
	}

	if mediaType != model.MediaMovie {
		http.Error(w, "only movie Seerr rearm payloads are supported in this version", http.StatusBadRequest)
		return
	}
	if arrID <= 0 && tmdbID <= 0 {
		http.Error(w, "payload requires arrId/radarrId/movieId or tmdbId", http.StatusBadRequest)
		return
	}

	requestKey := ""
	if requestID > 0 {
		requestKey = fmt.Sprintf("webhook:%d", requestID)
	} else if tmdbID > 0 {
		requestKey = fmt.Sprintf("webhook:%s:%d", mediaType, tmdbID)
	} else {
		requestKey = fmt.Sprintf("webhook:%s:arr:%d", mediaType, arrID)
	}
	raw, _ := json.Marshal(payload)
	_, _ = h.repo.UpsertSeerrRequest(r.Context(), db.SeerrRequestUpsert{Tenant: h.tenant, RequestKey: requestKey, MediaType: mediaType, TMDBID: tmdbID, ArrID: arrID, Title: title, Status: status, RawJSON: string(raw)})

	var item model.MediaCacheState
	var matched bool
	var err error
	if arrID > 0 {
		if force {
			item, err = h.repo.RequestRearm(r.Context(), h.tenant, mediaType, arrID)
			matched = err == nil
		} else {
			item, matched, err = h.repo.RequestRearmByArrIDIfArchived(r.Context(), h.tenant, mediaType, arrID)
		}
	} else {
		item, matched, err = h.repo.RequestRearmByTMDB(r.Context(), h.tenant, mediaType, tmdbID, force)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !matched {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": false, "message": "payload accepted, but no archived/tracked row matched yet", "tenant": h.tenant, "media_type": mediaType, "arr_id": arrID, "tmdb_id": tmdbID, "request_key": requestKey, "force": force})
		return
	}
	_ = h.repo.MarkSeerrRequestRearmed(r.Context(), h.tenant, requestKey)
	_ = h.repo.Event(r.Context(), item.ID, "seerr_webhook_rearm_requested", string(raw))
	slog.Info("Seerr POST requested rearm", "tenant", h.tenant, "media_type", mediaType, "arr_id", item.ArrID, "tmdb_id", tmdbID, "force", force, "request_key", requestKey)
	writeJSON(w, http.StatusAccepted, rearmResponse(item, "seerr_post"))
}

func (h *apiHandler) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	items, err := h.repo.ListState(r.Context(), h.tenant, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant": h.tenant, "items": items})
}

func (h *apiHandler) handleStateMovie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	arrID, ok := idFromPath(w, r.URL.Path, "/api/state/movie/")
	if !ok {
		return
	}
	item, found, err := h.repo.GetState(r.Context(), h.tenant, model.MediaMovie, arrID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "movie row not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant": h.tenant, "item": item})
}

func idFromPath(w http.ResponseWriter, path, prefix string) (int, bool) {
	rawID := strings.TrimPrefix(path, prefix)
	id, err := strconv.Atoi(strings.Trim(rawID, "/"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func rearmResponse(item model.MediaCacheState, source string) map[string]any {
	return map[string]any{"ok": true, "matched": true, "source": source, "tenant": item.Tenant, "media_type": item.MediaType, "arr_id": item.ArrID, "state": item.State, "rearm_requested": item.RearmRequested}
}

func (h *apiHandler) authorized(r *http.Request) bool {
	if !h.requireToken && h.token == "" {
		return true
	}
	if h.token == "" {
		return false
	}
	if r.Header.Get("X-Rehydrator-Token") == h.token {
		return true
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == h.token
}

func normalizeMediaType(s string) model.MediaType {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "media")
	s = strings.Trim(s, " _-")
	switch s {
	case "movie", "movies":
		return model.MediaMovie
	case "tv", "series", "show", "shows":
		return model.MediaSeries
	default:
		return ""
	}
}

func firstMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if v, ok := m[key].(map[string]any); ok {
			return v
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		case float64:
			return fmt.Sprintf("%.0f", t)
		case int:
			return fmt.Sprintf("%d", t)
		}
	}
	return ""
}

func firstInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		case string:
			var i int
			_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &i)
			if i != 0 {
				return i
			}
		}
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
func unauthorized(w http.ResponseWriter) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
