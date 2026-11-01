package gateway

import (
	"context"
	"crypto/subtle"
	"github.com/raefon/rehydrator/internal/dashboardauth"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
)

// ConfigureDashboard performs OIDC discovery before the HTTP listener starts.
func (s *Service) ConfigureDashboard(ctx context.Context, c dashboardauth.Config, ui http.Handler) (http.Handler, error) {
	if !c.Enabled {
		return ui, nil
	}
	auth, err := dashboardauth.New(ctx, c)
	if err != nil {
		return nil, err
	}
	s.dashboardOIDC = auth
	return auth.Wrap(ui), nil
}

// Dashboard uses write-capable qBittorrent credentials, never provider or DAV keys.
// External mode delegates access control to the configured ingress. Otherwise
// requests use explicit Basic auth with no application cookies or persisted tokens.
func (s *Service) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			http.Error(w, "origin rejected", 403)
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "origin rejected", 403)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Rehydrator-UI") != "1" {
		http.Error(w, "dashboard request header required", 403)
		return
	}
	mode := "basic"
	if s.dashboardExternalAuth {
		mode = "external"
	}
	if s.dashboardOIDC != nil {
		mode = "oidc"
	}
	if r.URL.Path == "/dashboard/api/auth" && r.Method == http.MethodGet {
		writeJSON(w, map[string]string{"mode": mode})
		return
	}
	user, password, ok := r.BasicAuth()
	if (s.dashboardOIDC != nil && !s.dashboardOIDC.Valid(r)) || (s.dashboardOIDC == nil && !s.dashboardExternalAuth && (!ok || s.qbUser == "" || s.qbPassword == "" || subtle.ConstantTimeCompare([]byte(user), []byte(s.qbUser)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(s.qbPassword)) != 1)) {
		// Deliberately no WWW-Authenticate: the UI owns the login form.
		http.Error(w, "unauthorized", 401)
		return
	}
	switch {
	case r.URL.Path == "/dashboard/api/snapshot" && r.Method == http.MethodGet:
		jobs, err := s.repo.CloudNZBs(r.Context(), s.tenant)
		if err != nil {
			http.Error(w, "catalog unavailable", 503)
			return
		}
		categories := []string{}
		for cat := range s.categories {
			categories = append(categories, cat)
		}
		sort.Strings(categories)
		rows := []dashboardJob{}
		for _, job := range jobs {
			rows = append(rows, s.dashboardJob(job))
		}
		writeJSON(w, map[string]any{"authMode": mode, "tenant": s.tenant, "role": s.role, "categories": categories, "jobs": rows})
	case r.URL.Path == "/dashboard/api/jobs" && r.Method == http.MethodPost:
		s.dashboardAdd(w, r)
	default:
		http.Error(w, "not found or method not allowed", 404)
	}
}

type dashboardFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}
type dashboardJob struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Category  string          `json:"category"`
	Protocol  string          `json:"protocol"`
	Status    string          `json:"status"`
	Files     []dashboardFile `json:"files"`
	Hidden    bool            `json:"hidden"`
	Attempts  int             `json:"attempts"`
	CreatedAt string          `json:"createdAt"`
}

func (s *Service) dashboardJob(j db.CloudNZBJob) dashboardJob {
	name := j.Name
	if name == "" {
		name = j.Filename
	}
	if name == "" {
		name = j.ID
	}
	result := dashboardJob{ID: j.ID, Name: name, Category: j.Category, Protocol: j.Protocol, Status: j.State, Hidden: j.Hidden, Attempts: j.Attempts, CreatedAt: j.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), Files: []dashboardFile{}}
	if j.State == "Completed" || j.State == "Mounting" {
		for _, file := range j.Files {
			result.Files = append(result.Files, dashboardFile{Name: file.Name, Size: file.Size, Path: s.catalogPath(j) + "/" + file.Name})
		}
	}
	return result
}
func (s *Service) dashboardAdd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 51<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, "invalid upload or upload exceeds 50 MiB", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	category := r.FormValue("category")
	if !s.categories[category] {
		http.Error(w, "unconfigured category", 400)
		return
	}
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	files := r.MultipartForm.File["upload"]
	if (magnet == "" && len(files) != 1) || (magnet != "" && len(files) != 0) {
		http.Error(w, "exactly one magnet or file required", 400)
		return
	}
	source := model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: magnet}
	if len(files) == 1 {
		h := files[0]
		f, err := h.Open()
		if err != nil {
			http.Error(w, "cannot read upload", 400)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (50<<20)+1))
		if err != nil || len(data) > 50<<20 {
			http.Error(w, "invalid upload or upload exceeds 50 MiB", 400)
			return
		}
		switch strings.ToLower(filepath.Ext(h.Filename)) {
		case ".nzb":
			source, err = validateUpload(data, h.Filename)
		case ".torrent":
			source = model.TorrentMetadata{Kind: model.SourceTorrentFile, TorrentFile: data, TorrentFilename: filepath.Base(h.Filename), SourceTitle: filepath.Base(h.Filename)}
			err = model.ValidateGatewayTorrent(data)
		default:
			http.Error(w, "torrent or NZB file required", 400)
			return
		}
		if err != nil {
			http.Error(w, "invalid source file", 400)
			return
		}
	}
	validated, err := source.ValidateSource()
	if err != nil {
		http.Error(w, "invalid source", 400)
		return
	}
	job, err := s.repo.AddGatewaySource(r.Context(), s.tenant, category, validated)
	if err != nil {
		http.Error(w, "unable to save submission", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, s.dashboardJob(job))
}
