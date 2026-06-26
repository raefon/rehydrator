package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Addr                         string
	Repo                         *db.Repo
	Tenant                       string
	Token                        string
	RequireToken                 bool
	MetricsEnabled               bool
	PlaybackEnabled              bool
	PlaybackRearmOnPlay          bool
	PlaybackCooldown             time.Duration
	PlaybackIgnoredTitles        []string
	PlaybackIgnoredTitleContains []string
	RefreshRadarr                func(context.Context) error
	RefreshSeerr                 func(context.Context) error
	PlexRefreshMovie             func(context.Context, int) error
	PlexRefreshMovies            func(context.Context) error
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
			repo:                         opt.Repo,
			tenant:                       opt.Tenant,
			token:                        opt.Token,
			requireToken:                 opt.RequireToken,
			metricsEnabled:               opt.MetricsEnabled,
			playbackEnabled:              opt.PlaybackEnabled,
			playbackRearmOnPlay:          opt.PlaybackRearmOnPlay,
			playbackCooldown:             opt.PlaybackCooldown,
			playbackIgnoredTitles:        normalizeIgnoredList(opt.PlaybackIgnoredTitles),
			playbackIgnoredTitleContains: normalizeIgnoredList(opt.PlaybackIgnoredTitleContains),
			refreshRadarr:                opt.RefreshRadarr,
			refreshSeerr:                 opt.RefreshSeerr,
			plexRefreshMovie:             opt.PlexRefreshMovie,
			plexRefreshMovies:            opt.PlexRefreshMovies,
		}
		mux.HandleFunc("/metrics", api.handleMetrics)
		mux.HandleFunc("/api/state/movie/", api.handleStateMovie)
		mux.HandleFunc("/api/prune/movie/", api.handlePruneMovie)
		mux.HandleFunc("/api/rearm/movie/tmdb/", api.handleRearmMovieTMDB)
		mux.HandleFunc("/api/rearm/movie/", api.handleRearmMovie)
		mux.HandleFunc("/api/refresh/radarr", api.handleRefreshRadarr)
		mux.HandleFunc("/api/refresh/seerr", api.handleRefreshSeerr)
		mux.HandleFunc("/api/plex/refresh/movie/", api.handlePlexRefreshMovie)
		mux.HandleFunc("/api/plex/refresh/movies", api.handlePlexRefreshMovies)
		mux.HandleFunc("/api/radarr/webhook", api.handleRadarrWebhook)
		mux.HandleFunc("/api/seerr/webhook", api.handleSeerrWebhook)
		mux.HandleFunc("/api/seerr/rearm", api.handleSeerrRearm)
		mux.HandleFunc("/api/playback/plex", api.handlePlexPlayback)
		mux.HandleFunc("/api/playback/event", api.handleGenericPlayback)
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
	repo                         *db.Repo
	tenant                       string
	token                        string
	requireToken                 bool
	metricsEnabled               bool
	playbackEnabled              bool
	playbackRearmOnPlay          bool
	playbackCooldown             time.Duration
	playbackIgnoredTitles        []string
	playbackIgnoredTitleContains []string
	refreshRadarr                func(context.Context) error
	refreshSeerr                 func(context.Context) error
	plexRefreshMovie             func(context.Context, int) error
	plexRefreshMovies            func(context.Context) error
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
	_, _ = fmt.Fprintf(w, "rehydrator_playback_intent_rows{tenant=%q} %d\n", h.tenant, snap.PlaybackIntentRows)
	_, _ = fmt.Fprintf(w, "rehydrator_playback_intents_total{tenant=%q} %d\n", h.tenant, snap.PlaybackIntentTotal)
	_, _ = fmt.Fprintf(w, "rehydrator_unmatched_playback_intents_total{tenant=%q} %d\n", h.tenant, snap.UnmatchedPlaybackTotal)
	_, _ = fmt.Fprintf(w, "rehydrator_unmatched_playback_intents_open{tenant=%q} %d\n", h.tenant, snap.UnmatchedPlaybackOpen)
	_, _ = fmt.Fprintf(w, "rehydrator_playback_ignored_total{tenant=%q} %d\n", h.tenant, snap.PlaybackIgnoredTotal)
	_, _ = fmt.Fprintf(w, "rehydrator_waiting_visibility_items{tenant=%q} %d\n", h.tenant, snap.WaitingVisibilityItems)
	_, _ = fmt.Fprintf(w, "rehydrator_provider_cooldowns_active{tenant=%q} %d\n", h.tenant, snap.ProviderCooldownsActive)
	_, _ = fmt.Fprintf(w, "rehydrator_plex_refresh_total{tenant=%q} %d\n", h.tenant, snap.PlexRefreshTotal)
	_, _ = fmt.Fprintf(w, "rehydrator_plex_refresh_failures_total{tenant=%q} %d\n", h.tenant, snap.PlexRefreshFailures)
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

func (h *apiHandler) handlePlexRefreshMovie(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	if h.plexRefreshMovie == nil {
		http.Error(w, "plex refresh is not configured", http.StatusNotFound)
		return
	}
	arrID, ok := idFromPath(w, r.URL.Path, "/api/plex/refresh/movie/")
	if !ok {
		return
	}
	if err := h.plexRefreshMovie(r.Context(), arrID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "refresh": "plex_movie", "arr_id": arrID})
}

func (h *apiHandler) handlePlexRefreshMovies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	if h.plexRefreshMovies == nil {
		http.Error(w, "plex refresh is not configured", http.StatusNotFound)
		return
	}
	if err := h.plexRefreshMovies(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "refresh": "plex_movies"})
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

func (h *apiHandler) handleRadarrWebhook(w http.ResponseWriter, r *http.Request) {
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
	raw, _ := json.Marshal(payload)
	movie := firstMap(payload, "movie", "Movie")
	movieFile := firstMap(payload, "movieFile", "MovieFile", "movie_file")
	arrID := firstInt(movie, "id", "movieId", "radarrId")
	if arrID == 0 {
		arrID = firstInt(payload, "movieId", "radarrId", "arrId")
	}
	tmdbID := firstInt(movie, "tmdbId", "tmdbID")
	if tmdbID == 0 {
		tmdbID = firstInt(payload, "tmdbId", "tmdbID")
	}
	title := firstNonEmptyString(firstString(movie, "title", "name"), firstString(payload, "title", "movieTitle"))
	eventType := firstString(payload, "eventType", "event", "type")
	importedPath := firstNonEmptyString(firstString(movieFile, "path"), firstString(payload, "movieFilePath", "path"))

	if arrID > 0 {
		item, err := h.repo.UpsertRequestedRadarrMovie(r.Context(), h.tenant, arrID, title, tmdbID)
		if err == nil {
			_ = h.repo.Event(r.Context(), item.ID, "radarr_webhook_received", string(raw))
		}
	}

	refreshed := false
	if h.refreshRadarr != nil {
		if err := h.refreshRadarr(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		refreshed = true
	}

	item, found, err := h.findPlaybackMatch(r.Context(), playbackIntent{MediaType: model.MediaMovie, ArrID: arrID, TMDBID: tmdbID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if found {
		_ = h.repo.Event(r.Context(), item.ID, "radarr_webhook_seeded", string(raw))
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": true, "refresh": refreshed, "event": eventType, "arr_id": item.ArrID, "tmdb_id": valueInt(item.TMDBID), "state": item.State, "imported_path": importedPath})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": false, "refresh": refreshed, "event": eventType, "arr_id": arrID, "tmdb_id": tmdbID, "title": title})
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
	if tmdbID > 0 {
		placeholder, created, err := h.repo.UpsertRequestedMoviePlaceholder(r.Context(), h.tenant, tmdbID, title, status)
		if err == nil && created {
			_ = h.repo.Event(r.Context(), placeholder.ID, "seerr_webhook_placeholder_created", string(raw))
			slog.Info("Seerr webhook created requested placeholder", "tenant", h.tenant, "tmdb_id", tmdbID, "arr_id", placeholder.ArrID, "title", title)
		}
	}

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

type playbackIntent struct {
	Source    string
	Event     string
	MediaType model.MediaType
	ArrID     int
	TMDBID    int
	Title     string
	User      string
	RawJSON   string
}

func (h *apiHandler) handleGenericPlayback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	if !h.playbackEnabled {
		http.Error(w, "playback rearm is disabled", http.StatusNotFound)
		return
	}
	defer r.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}
	intent := playbackIntentFromGeneric(payload)
	h.handlePlaybackIntent(w, r, intent)
}

func (h *apiHandler) handlePlexPlayback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.authorized(r) {
		unauthorized(w)
		return
	}
	if !h.playbackEnabled {
		http.Error(w, "playback rearm is disabled", http.StatusNotFound)
		return
	}

	payload, raw, err := decodePlexWebhookPayload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	intent := playbackIntentFromPlex(payload, raw)
	h.handlePlaybackIntent(w, r, intent)
}

func (h *apiHandler) handlePlaybackIntent(w http.ResponseWriter, r *http.Request, intent playbackIntent) {
	if intent.MediaType == "" {
		intent.MediaType = model.MediaMovie
	}
	if intent.MediaType != model.MediaMovie {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": false, "ignored": true, "message": "only movie playback rearm is supported in this version", "media_type": intent.MediaType})
		return
	}
	if !isPlaybackStartLike(intent.Event) {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": false, "ignored": true, "message": "playback event ignored", "event": intent.Event})
		return
	}
	if ignored, reason := h.isIgnoredPlaybackIntent(intent); ignored {
		_ = h.repo.RecordIgnoredPlaybackIntent(r.Context(), db.PlaybackIgnoredIntent{Tenant: h.tenant, Source: intent.Source, Event: intent.Event, Title: intent.Title, Reason: reason, RawJSON: intent.eventJSON()})
		slog.Debug("playback intent ignored", "tenant", h.tenant, "source", intent.Source, "event", intent.Event, "title", intent.Title, "reason", reason)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matched": false, "ignored": true, "action": "ignored", "reason": reason, "source": intent.Source, "event": intent.Event, "title": intent.Title})
		return
	}

	item, found, err := h.findPlaybackMatch(r.Context(), intent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	refreshed := false
	if !found {
		_ = h.repo.RecordUnmatchedPlaybackIntent(r.Context(), db.PlaybackIntentUpsert{Tenant: h.tenant, MediaType: intent.MediaType, ArrID: intent.ArrID, TMDBID: intent.TMDBID, Source: intent.Source, Event: intent.Event, Title: intent.Title, User: intent.User, RawJSON: intent.eventJSON()})
		if h.refreshRadarr != nil {
			if err := h.refreshRadarr(r.Context()); err != nil {
				slog.Warn("playback unmatched Radarr refresh failed", "tenant", h.tenant, "source", intent.Source, "event", intent.Event, "tmdb_id", intent.TMDBID, "arr_id", intent.ArrID, "error", err)
			} else {
				refreshed = true
				item, found, err = h.findPlaybackMatch(r.Context(), intent)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
	}

	if !found {
		slog.Info("playback intent accepted but no tracked movie matched", "tenant", h.tenant, "source", intent.Source, "event", intent.Event, "tmdb_id", intent.TMDBID, "arr_id", intent.ArrID, "title", intent.Title, "radarr_refreshed", refreshed)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": false, "source": intent.Source, "event": intent.Event, "media_type": intent.MediaType, "arr_id": intent.ArrID, "tmdb_id": intent.TMDBID, "radarr_refreshed": refreshed, "message": "playback intent stored; no tracked movie row matched yet"})
		return
	}

	_ = h.repo.MarkPlaybackIntentMatched(r.Context(), h.tenant, intent.MediaType, intent.TMDBID, item.ArrID, item.ID)
	_ = h.repo.RecordPlaybackIntent(r.Context(), item.ID)
	_ = h.repo.Event(r.Context(), item.ID, "playback_intent_received", intent.eventJSON())

	if item.ArrID < 0 || item.SymlinkPath == "" {
		_ = h.repo.Event(r.Context(), item.ID, "playback_ignored_requested_placeholder", intent.eventJSON())
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": true, "action": "requested_placeholder", "arr_id": item.ArrID, "tmdb_id": valueInt(item.TMDBID), "state": item.State, "radarr_refreshed": refreshed})
		return
	}

	if item.State == model.StateAvailable || item.State == model.StateHot || item.State == model.StateCooling {
		_ = h.repo.Event(r.Context(), item.ID, "playback_ignored_available", intent.eventJSON())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matched": true, "action": "already_available", "arr_id": item.ArrID, "tmdb_id": valueInt(item.TMDBID), "state": item.State, "radarr_refreshed": refreshed})
		return
	}

	if h.playbackCooldown > 0 && item.LastPlayIntentAt != nil && (item.RearmRequested || item.State == model.StateRearming) && time.Since(*item.LastPlayIntentAt) < h.playbackCooldown {
		_ = h.repo.Event(r.Context(), item.ID, "playback_ignored_cooldown", intent.eventJSON())
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": true, "action": "cooldown", "arr_id": item.ArrID, "state": item.State, "cooldown_seconds": int(h.playbackCooldown.Seconds())})
		return
	}

	if !h.playbackRearmOnPlay {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": true, "action": "intent_recorded", "arr_id": item.ArrID, "state": item.State, "rearm_on_play": false})
		return
	}

	if err := h.repo.MarkPlaybackRearmRequested(r.Context(), item.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.repo.Event(r.Context(), item.ID, "playback_rearm_requested", intent.eventJSON())
	slog.Info("playback intent requested rearm", "tenant", h.tenant, "source", intent.Source, "event", intent.Event, "media_type", intent.MediaType, "arr_id", item.ArrID, "tmdb_id", valueInt(item.TMDBID), "title", firstNonEmptyString(intent.Title, valueString(item.ArrTitle)))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "matched": true, "action": "rearm_requested", "arr_id": item.ArrID, "tmdb_id": valueInt(item.TMDBID), "state": item.State, "source": intent.Source, "radarr_refreshed": refreshed})
}

func (h *apiHandler) isIgnoredPlaybackIntent(intent playbackIntent) (bool, string) {
	title := strings.ToLower(strings.TrimSpace(intent.Title))
	if title == "" {
		return false, ""
	}
	for _, ignored := range h.playbackIgnoredTitles {
		if title == ignored {
			return true, "ignored_title"
		}
	}
	for _, contains := range h.playbackIgnoredTitleContains {
		if contains != "" && strings.Contains(title, contains) {
			return true, "ignored_title_contains"
		}
	}
	return false, ""
}

func normalizeIgnoredList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (h *apiHandler) findPlaybackMatch(ctx context.Context, intent playbackIntent) (model.MediaCacheState, bool, error) {
	if intent.ArrID > 0 {
		return h.repo.GetState(ctx, h.tenant, intent.MediaType, intent.ArrID)
	}
	if intent.TMDBID > 0 {
		return h.repo.GetStateByTMDB(ctx, h.tenant, intent.MediaType, intent.TMDBID)
	}
	return model.MediaCacheState{}, false, nil
}

func decodePlexWebhookPayload(r *http.Request) (map[string]any, string, error) {
	defer r.Body.Close()
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, "", fmt.Errorf("invalid plex multipart payload: %w", err)
		}
		raw := r.FormValue("payload")
		if strings.TrimSpace(raw) == "" {
			return nil, "", fmt.Errorf("plex webhook missing payload field")
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, raw, fmt.Errorf("invalid plex payload json: %w", err)
		}
		return payload, raw, nil
	}
	rawBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	raw := string(rawBytes)
	var payload map[string]any
	if err := json.Unmarshal(rawBytes, &payload); err != nil {
		return nil, raw, fmt.Errorf("invalid json payload: %w", err)
	}
	return payload, raw, nil
}

func playbackIntentFromGeneric(payload map[string]any) playbackIntent {
	mediaType := normalizeMediaType(firstString(payload, "media_type", "mediaType", "type"))
	media := firstMap(payload, "media", "metadata", "Metadata")
	if mediaType == "" {
		mediaType = normalizeMediaType(firstString(media, "media_type", "mediaType", "type"))
	}
	tmdbID := firstInt(payload, "tmdb_id", "tmdbId", "tmdbID")
	if tmdbID == 0 {
		tmdbID = firstInt(media, "tmdb_id", "tmdbId", "tmdbID")
	}
	arrID := firstInt(payload, "arr_id", "arrId", "radarrId", "movieId")
	if arrID == 0 {
		arrID = firstInt(media, "arr_id", "arrId", "radarrId", "movieId")
	}
	title := firstNonEmptyString(firstString(payload, "title", "mediaName"), firstString(media, "title", "name"))
	raw, _ := json.Marshal(payload)
	return playbackIntent{Source: firstNonEmptyString(firstString(payload, "source"), "generic"), Event: firstString(payload, "event", "type"), MediaType: mediaType, ArrID: arrID, TMDBID: tmdbID, Title: title, User: firstString(payload, "user", "username"), RawJSON: string(raw)}
}

func playbackIntentFromPlex(payload map[string]any, raw string) playbackIntent {
	meta := firstMap(payload, "Metadata", "metadata")
	mediaType := normalizeMediaType(firstString(meta, "type", "mediaType"))
	tmdbID := firstInt(meta, "tmdbId", "tmdbID")
	if tmdbID == 0 {
		tmdbID = extractExternalID(meta, "tmdb")
	}
	player := firstMap(payload, "Player", "player")
	account := firstMap(payload, "Account", "account")
	return playbackIntent{Source: "plex", Event: firstString(payload, "event"), MediaType: mediaType, TMDBID: tmdbID, Title: firstString(meta, "title"), User: firstNonEmptyString(firstString(account, "title", "username"), firstString(player, "title")), RawJSON: raw}
}

func (i playbackIntent) eventJSON() string {
	payload := map[string]any{"source": i.Source, "event": i.Event, "media_type": i.MediaType, "arr_id": i.ArrID, "tmdb_id": i.TMDBID, "title": i.Title, "user": i.User}
	if i.RawJSON != "" {
		payload["raw"] = json.RawMessage(i.RawJSON)
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func isPlaybackStartLike(event string) bool {
	e := strings.ToLower(strings.TrimSpace(event))
	if e == "" {
		return true
	}
	return strings.Contains(e, "play") || strings.Contains(e, "resume") || strings.Contains(e, "start")
}

func extractExternalID(m map[string]any, provider string) int {
	provider = strings.ToLower(provider)
	for _, key := range []string{"Guid", "guid", "guids", "GUID"} {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if id := parseProviderID(t, provider); id > 0 {
				return id
			}
		case []any:
			for _, one := range t {
				switch g := one.(type) {
				case map[string]any:
					if id := parseProviderID(firstString(g, "id", "guid"), provider); id > 0 {
						return id
					}
				case string:
					if id := parseProviderID(g, provider); id > 0 {
						return id
					}
				}
			}
		}
	}
	return 0
}

func parseProviderID(s string, provider string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{provider + "://", provider + ":", "com.plexapp.agents.themoviedb://"} {
		if strings.Contains(s, prefix) {
			idx := strings.Index(s, prefix)
			rest := s[idx+len(prefix):]
			rest = strings.TrimLeft(rest, "/")
			parts := strings.FieldsFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
			if len(parts) > 0 {
				if id, err := strconv.Atoi(parts[0]); err == nil {
					return id
				}
			}
		}
	}
	return 0
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
	if r.URL.Query().Get("token") == h.token {
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

func firstNonEmptyString(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func valueInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func valueString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
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
