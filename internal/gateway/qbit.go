package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func sessionHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

// QBittorrent exposes the download-client subset used by Arr. Unsupported
// operations fail explicitly rather than pretending to control the provider.
func (s *Service) QBittorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 51<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if r.ParseMultipartForm(1<<20) != nil {
			http.Error(w, "invalid upload", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
	} else if r.ParseForm() != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	endpoint := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host {
			http.Error(w, "origin rejected", 403)
			return
		}
	}
	if endpoint == "auth/login" {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.FormValue("username")), []byte(s.qbUser)) != 1 || subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.qbPassword)) != 1 {
			http.Error(w, "Fails.", 403)
			return
		}
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			w.WriteHeader(500)
			return
		}
		sid := hex.EncodeToString(b)
		if s.repo.GatewaySession(r.Context(), s.tenant, sessionHash(sid), true) != nil {
			w.WriteHeader(503)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid, Path: "/api/v2/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		_, _ = io.WriteString(w, "Ok.")
		return
	}
	cookie, err := r.Cookie("SID")
	if err != nil || s.repo.GatewaySession(r.Context(), s.tenant, sessionHash(cookie.Value), false) != nil {
		http.Error(w, "unauthorized", 403)
		return
	}
	read := endpoint == "app/version" || endpoint == "app/webapiVersion" || endpoint == "app/preferences" || endpoint == "torrents/info" || endpoint == "torrents/files" || endpoint == "torrents/properties" || endpoint == "torrents/categories" || endpoint == "gateway/jobs"
	if !read && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	switch endpoint {
	case "gateway/jobs":
		jobs, e := s.repo.GatewayJobs(r.Context(), s.tenant)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		out := []map[string]any{}
		for _, j := range jobs {
			out = append(out, map[string]any{"id": j.ID, "protocol": j.Protocol, "category": j.Category, "name": j.Name, "state": j.State, "provider_id": j.ProviderID, "infohash": j.InfoHash, "attempts": j.Attempts, "missing_checks": j.MissingChecks, "hidden": j.Hidden, "files": len(j.Files), "error": j.LastError})
		}
		writeJSON(w, out)
	case "gateway/reconcile":
		j, e := s.repo.GatewayJob(r.Context(), s.tenant, r.FormValue("id"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if e = s.repo.WakeGatewayJob(r.Context(), s.tenant, j.ID); e != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(202)
	case "auth/logout":
		if err = s.repo.GatewayLogout(r.Context(), s.tenant, sessionHash(cookie.Value)); err != nil {
			w.WriteHeader(503)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "", Path: "/api/v2/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(200)
	case "app/version":
		_, _ = io.WriteString(w, "v4.6.7")
	case "app/webapiVersion":
		_, _ = io.WriteString(w, "2.8.3")
	case "app/preferences":
		writeJSON(w, map[string]any{"save_path": s.cfg.DownloadPath, "temp_path_enabled": false, "queueing_enabled": false, "max_active_downloads": 5, "max_active_torrents": 5, "web_ui_username": s.qbUser})
	case "torrents/categories":
		cats := map[string]any{}
		for cat := range s.categories {
			cats[cat] = map[string]any{"name": cat, "savePath": filepath.Join(s.cfg.DownloadPath, cat)}
		}
		writeJSON(w, cats)
	case "torrents/createCategory":
		if !s.categories[r.FormValue("category")] {
			http.Error(w, "configure category in Rehydrator", 409)
			return
		}
		w.WriteHeader(200)
	case "torrents/topPrio":
		http.Error(w, "provider queue priority is managed by TorBox", 409)
	case "torrents/setCategory":
		category := r.FormValue("category")
		if !s.categories[category] {
			http.Error(w, "unconfigured category", 409)
			return
		}
		for _, hash := range strings.Split(r.FormValue("hashes"), "|") {
			if err = s.repo.GatewayCategory(r.Context(), s.tenant, hash, category); err != nil {
				http.Error(w, "cannot change category after submission", 409)
				return
			}
		}
		w.WriteHeader(200)
	case "torrents/add":
		for _, field := range []string{"paused", "stopped", "sequentialDownload", "firstLastPiecePrio"} {
			if r.FormValue(field) == "true" {
				http.Error(w, "unsupported torrent option: "+field, 409)
				return
			}
		}
		if layout := r.FormValue("contentLayout"); layout != "" && layout != "Original" {
			http.Error(w, "use Original content layout", 409)
			return
		}
		for _, field := range []string{"ratioLimit", "seedingTimeLimit", "inactiveSeedingTimeLimit"} {
			if value := r.FormValue(field); value != "" && value != "-2" {
				http.Error(w, "TorBox manages seeding limits", 409)
				return
			}
		}
		cat := r.FormValue("category")
		if !s.categories[cat] {
			http.Error(w, "unconfigured category", 400)
			return
		}
		sources := []model.TorrentMetadata{}
		for _, magnet := range strings.Fields(r.FormValue("urls")) {
			sources = append(sources, model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: magnet, SourceTitle: r.FormValue("rename")})
		}
		if r.MultipartForm != nil {
			for _, h := range r.MultipartForm.File["torrents"] {
				f, e := h.Open()
				if e != nil {
					w.WriteHeader(400)
					return
				}
				b, e := io.ReadAll(io.LimitReader(f, (50<<20)+1))
				f.Close()
				if e != nil {
					w.WriteHeader(400)
					return
				}
				sources = append(sources, model.TorrentMetadata{Kind: model.SourceTorrentFile, TorrentFile: b, TorrentFilename: h.Filename, SourceTitle: r.FormValue("rename")})
			}
		}
		if len(sources) != 1 {
			http.Error(w, "exactly one torrent file or magnet required", 400)
			return
		}
		if _, err = s.repo.AddGatewaySource(r.Context(), s.tenant, cat, sources[0]); err != nil {
			http.Error(w, "could not validate or persist source", 400)
			return
		}
		_, _ = io.WriteString(w, "Ok.")
	case "torrents/info":
		jobs, e := s.repo.GatewayJobs(r.Context(), s.tenant)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		items := []map[string]any{}
		for _, j := range jobs {
			if j.Protocol != "torrent" || j.Hidden {
				continue
			}
			if cat := r.FormValue("category"); cat != "" && cat != j.Category {
				continue
			}
			if hashes := r.FormValue("hashes"); hashes != "" && !containsHash(hashes, j.InfoHash) {
				continue
			}
			state := "downloading"
			progress := 0.0
			switch j.State {
			case "Completed":
				state = "uploading"
				progress = 1
			case "Queued", "Missing":
				state = "queuedDL"
			case "Submitting":
				state = "metaDL"
			case "Mounting":
				state = "checkingDL"
			case "SubmissionUnknown":
				state = "pausedDL"
			case "Failed":
				state = "error"
			}
			var size int64
			for _, f := range j.Files {
				size += f.Size
			}
			items = append(items, map[string]any{"hash": j.InfoHash, "name": j.Name, "category": j.Category, "state": state, "progress": progress, "size": size, "total_size": size, "amount_left": int64(float64(size) * (1 - progress)), "save_path": s.torrentSavePath(j), "content_path": s.storage(j), "added_on": j.CreatedAt.Unix(), "completion_on": j.CreatedAt.Unix(), "dlspeed": 0, "upspeed": 0, "ratio": 0, "eta": 8640000, "priority": 1, "num_seeds": 0, "num_leechs": 0, "tags": ""})
		}
		writeJSON(w, items)
	case "torrents/files", "torrents/properties":
		j, e := s.repo.GatewayJob(r.Context(), s.tenant, r.FormValue("hash"))
		if e != nil || j.Protocol != "torrent" {
			http.NotFound(w, r)
			return
		}
		var size int64
		files := []map[string]any{}
		for i, f := range j.Files {
			size += f.Size
			p := 0
			if j.State == "Completed" {
				p = 1
			}
			files = append(files, map[string]any{"index": i, "name": filepath.ToSlash(filepath.Join(j.ID, filepath.FromSlash(f.Name))), "size": f.Size, "progress": p, "priority": 1, "is_seed": p == 1})
		}
		if endpoint == "torrents/files" {
			writeJSON(w, files)
		} else {
			writeJSON(w, map[string]any{"save_path": s.torrentSavePath(j), "total_size": size, "seeding_time": 0, "share_ratio": 0})
		}
	case "torrents/delete":

		for _, hash := range strings.Split(r.FormValue("hashes"), "|") {
			j, e := s.repo.GatewayJob(r.Context(), s.tenant, hash)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			if r.FormValue("deleteFiles") == "true" {
				if s.role == config.RoleGateway {
					e = s.repo.RequestGatewayCleanup(r.Context(), s.tenant, j.ID)
				} else {
					e = s.removeStagedLinks(j)
				}
				if e != nil {
					http.Error(w, "staging cleanup blocked", 409)
					return
				}
			}
			if e = s.repo.HideGatewayJob(r.Context(), s.tenant, j.ID); e != nil {
				w.WriteHeader(503)
				return
			}
		}
		w.WriteHeader(200)
	default:
		http.Error(w, "unsupported gateway operation", 501)
	}
}
func containsHash(hashes, hash string) bool {
	for _, h := range strings.Split(hashes, "|") {
		if strings.EqualFold(h, hash) {
			return true
		}
	}
	return false
}

// Delete-data affects only known staging symlinks. Catalog, cloud payload and
// library imports remain untouched so an Arr queue cleanup cannot evict media.
func (s *Service) removeStagedLinks(j db.GatewayJob) error {
	for _, f := range j.Files {
		dest := filepath.Join(s.storage(j), filepath.FromSlash(f.Name))
		if err := safeStagingParents(s.cfg.DownloadPath, filepath.Dir(dest)); err != nil {
			return err
		}
		target, err := os.Readlink(dest)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		expected := filepath.Join(s.cfg.MountPath, s.catalogPath(j), filepath.FromSlash(f.Name))
		if target != expected {
			return fmt.Errorf("unexpected staging link")
		}
		if err = os.Remove(dest); err != nil {
			return err
		}
	}
	return nil
}

// qBittorrent file names are relative to save_path. The stable job directory is
// the per-torrent content root, never the client's/category's base directory.
func (s *Service) torrentSavePath(j db.GatewayJob) string {
	return filepath.Join(s.cfg.DownloadPath, j.Category)
}
