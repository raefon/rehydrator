package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

const SABPath = "/sabnzbd-cloud/api"
const DAVPath = "/cloud-usenet/dav/"

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Service) SAB(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 51<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "invalid or oversized upload", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", 400)
			return
		}
	}
	if subtle.ConstantTimeCompare([]byte(r.FormValue("apikey")), []byte(s.cfg.SABAPIKey)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	mode := r.FormValue("mode")
	switch mode {
	case "version":
		writeJSON(w, map[string]any{"version": "4.4.0"})
	case "fullstatus":
		writeJSON(w, map[string]any{"status": map[string]any{"version": "4.4.0", "paused": false, "diskspace1": "1000", "diskspace2": "1000", "diskspacetotal1": "1000", "complete_dir": s.cfg.DownloadPath}})
	case "get_config":
		cats := []map[string]any{}
		for cat := range s.categories {
			cats = append(cats, map[string]any{"name": cat, "dir": filepath.Join(s.cfg.DownloadPath, cat), "pp": 3, "priority": 0})
		}
		writeJSON(w, map[string]any{"config": map[string]any{"categories": cats, "misc": map[string]any{"complete_dir": s.cfg.DownloadPath, "enable_tv_sorting": false, "enable_movie_sorting": false}}})
	case "addfile":
		category := r.FormValue("cat")
		if !s.categories[category] {
			writeJSON(w, map[string]any{"status": false, "error": "category is not configured"})
			return
		}
		if r.MultipartForm == nil || len(r.MultipartForm.File["name"]) != 1 {
			writeJSON(w, map[string]any{"status": false, "error": "exactly one NZB upload is required"})
			return
		}
		f, h, err := r.FormFile("name")
		if err != nil {
			writeJSON(w, map[string]any{"status": false, "error": "one NZB upload is required"})
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (50<<20)+1))
		if err != nil || len(data) > 50<<20 {
			http.Error(w, "NZB too large", 413)
			return
		}
		source, err := validateUpload(data, h.Filename)
		if err != nil {
			writeJSON(w, map[string]any{"status": false, "error": "invalid NZB source"})
			return
		}
		job, err := s.repo.AddCloudNZB(r.Context(), s.tenant, category, source.NZBFilename, source.NZBFile)
		if err != nil {
			http.Error(w, "could not persist cloud NZB", 500)
			return
		}
		writeJSON(w, map[string]any{"status": true, "nzo_ids": []string{job.ID}})
	case "queue", "history":
		if r.FormValue("name") == "delete" {
			id := r.FormValue("value")
			if !IsJob(id) {
				writeJSON(w, map[string]any{"status": false})
				return
			}
			if err := s.repo.HideCloudNZB(r.Context(), s.tenant, strings.ToLower(id)); err != nil {
				http.Error(w, "queue update failed", 500)
				return
			}
			writeJSON(w, map[string]any{"status": true})
			return
		}
		jobs, err := s.repo.CloudNZBs(r.Context(), s.tenant)
		if err != nil {
			http.Error(w, "cloud status unavailable", 500)
			return
		}
		slots := []map[string]any{}
		for _, j := range jobs {
			category := r.FormValue("cat")
			if category == "" {
				category = r.FormValue("category")
			}
			if j.Protocol != "usenet" {
				continue
			}
			if j.Hidden || (category != "" && j.Category != category) {
				continue
			}
			if filter := r.FormValue("nzo_ids"); filter != "" && !strings.Contains(","+filter+",", ","+j.ID+",") {
				continue
			}
			terminal := j.State == "Completed" || j.State == "Failed"
			if (mode == "history") != terminal {
				continue
			}
			status := j.State
			if !terminal {
				if status == "Queued" || status == "Missing" {
					status = "Queued"
				} else {
					status = "Downloading"
				}
			}
			if j.State == "SubmissionUnknown" {
				status = "Paused"
			}
			var size int64
			for _, f := range j.Files {
				size += f.Size
			}
			slots = append(slots, map[string]any{"nzo_id": j.ID, "name": j.Name, "filename": j.Name, "nzb_name": j.Filename, "status": status, "category": j.Category, "cat": j.Category, "storage": s.storage(j), "bytes": size, "fail_message": j.LastError, "completed": j.CreatedAt.Unix(), "download_time": 1, "percentage": "0", "mb": "0", "mbleft": "0", "timeleft": "0:00:00", "priority": "Normal"})
		}
		writeJSON(w, map[string]any{mode: map[string]any{"slots": slots, "noofslots": len(slots), "noofslots_total": len(slots), "status": "Downloading", "paused": false, "diskspace1": "1000", "diskspace2": "1000", "speed": "0", "kbpersec": "0", "mb": "0", "mbleft": "0", "timeleft": "0:00:00"}})
	default:
		writeJSON(w, map[string]any{"status": false, "error": "unsupported mode; use NZB file uploads"})
	}
}
