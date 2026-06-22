package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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
	Addr   string
	Repo   *db.Repo
	Tenant string
	Token  string
}

func NewServer(addr string) *Server {
	return newServer(addr, nil, "", "")
}

func NewAPIServer(opt APIOptions) *Server {
	return newServer(opt.Addr, opt.Repo, opt.Tenant, opt.Token)
}

func newServer(addr string, repo *db.Repo, tenant string, token string) *Server {
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

	if repo != nil {
		api := &apiHandler{repo: repo, tenant: tenant, token: token}
		mux.HandleFunc("/api/rearm/movie/", api.handleRearmMovie)
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
	repo   *db.Repo
	tenant string
	token  string
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

	rawID := strings.TrimPrefix(r.URL.Path, "/api/rearm/movie/")
	arrID, err := strconv.Atoi(strings.Trim(rawID, "/"))
	if err != nil || arrID <= 0 {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	item, err := h.repo.RequestRearm(r.Context(), h.tenant, model.MediaMovie, arrID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":              true,
		"tenant":          item.Tenant,
		"media_type":      item.MediaType,
		"arr_id":          item.ArrID,
		"state":           item.State,
		"rearm_requested": item.RearmRequested,
	})
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

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant": h.tenant,
		"items":  items,
	})
}

func (h *apiHandler) authorized(r *http.Request) bool {
	if h.token == "" {
		return true
	}
	if r.Header.Get("X-Rehydrator-Token") == h.token {
		return true
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == h.token
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func unauthorized(w http.ResponseWriter) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
