package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var streamHTTPClient = &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: 90 * time.Second}}

type cachedLink struct {
	url   string
	until time.Time
}
type linkCache struct {
	mu      sync.Mutex
	entries map[string]cachedLink
}

func (s *Service) deliveryLink(ctx context.Context, protocol string, providerID, fileID int64, refresh bool) (string, error) {
	s.links.mu.Lock()
	defer s.links.mu.Unlock()
	key := fmt.Sprintf("%s:%d:%d", protocol, providerID, fileID)
	if !refresh {
		if cached, ok := s.links.entries[key]; ok && time.Now().Before(cached.until) {
			return cached.url, nil
		}
	}
	var link string
	var err error
	if protocol == "torrent" {
		link, err = s.torrent.Link(ctx, providerID, fileID)
	} else {
		link, err = s.provider.Link(ctx, providerID, fileID)
	}
	if err != nil {
		return "", err
	}
	if s.links.entries == nil {
		s.links.entries = map[string]cachedLink{}
	}
	for k, v := range s.links.entries {
		if time.Now().After(v.until) {
			delete(s.links.entries, k)
		}
	}
	s.links.entries[key] = cachedLink{link, time.Now().Add(20 * time.Minute)}
	return link, nil
}

type davEntry struct {
	name           string
	protocol       string
	ready          bool
	directory      bool
	size           int64
	provider, file int64
}

func (s *Service) DAV(w http.ResponseWriter, r *http.Request) {
	user, password, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.DAVUsername)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.DAVPassword)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="TorBox cloud Usenet"`)
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Method == "OPTIONS" {
		w.Header().Set("DAV", "1")
		w.Header().Set("Allow", "OPTIONS, PROPFIND, HEAD, GET")
		w.WriteHeader(200)
		return
	}
	if r.Method != "PROPFIND" && r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, s.DAVRoot())
	relative = strings.TrimSuffix(relative, "/")
	if relative != "" && (path.Clean(relative) != relative || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, "\\\x00")) {
		http.Error(w, "invalid path", 400)
		return
	}
	jobs, err := s.repo.GatewayJobs(r.Context(), s.tenant)
	if err != nil {
		http.Error(w, "catalog unavailable", 503)
		return
	}
	entries := map[string]davEntry{"": {directory: true}, "jobs": {name: "jobs", directory: true}}
	{
		delete(entries, "jobs")
		entries["torrents"] = davEntry{name: "torrents", directory: true}
		entries["usenet"] = davEntry{name: "usenet", directory: true}
	}
	for _, job := range jobs {

		if len(job.Files) == 0 {
			continue
		}
		base := s.catalogPath(job)
		{
			root := strings.Split(base, "/")[0]
			entries[root] = davEntry{name: root, directory: true}
			delete(entries, "jobs")
		}
		entries[base] = davEntry{name: job.ID, directory: true}
		for _, f := range job.Files {
			name := base + "/" + f.Name
			entries[name] = davEntry{name: path.Base(name), size: f.Size, protocol: job.Protocol, ready: job.State == "Completed" || job.State == "Mounting", provider: job.ProviderID, file: f.ID}
			for parent := path.Dir(name); parent != "." && parent != "jobs" && parent != base; parent = path.Dir(parent) {
				entries[parent] = davEntry{name: path.Base(parent), directory: true}
			}
		}
	}
	entry, found := entries[relative]
	if !found {
		http.NotFound(w, r)
		return
	}
	if r.Method == "PROPFIND" {
		depth := r.Header.Get("Depth")
		if depth == "" {
			depth = "1"
		}
		if depth != "0" && depth != "1" {
			http.Error(w, "only Depth 0 or 1 is supported", 403)
			return
		}
		names := []string{relative}
		if entry.directory && depth == "1" {
			for name := range entries {
				if name != relative && ((relative == "" && path.Dir(name) == ".") || path.Dir(name) == relative) {
					names = append(names, name)
				}
			}
		}
		sort.Strings(names)
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(207)
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">`)
		escape := func(v string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(v)); return b.String() }
		for _, name := range names {
			e := entries[name]
			href := s.DAVRoot()
			if name != "" {
				parts := strings.Split(name, "/")
				for i, p := range parts {
					parts[i] = url.PathEscape(p)
				}
				href += strings.Join(parts, "/")
			}
			kind := ""
			if e.directory {
				kind = "<D:collection/>"
				if !strings.HasSuffix(href, "/") {
					href += "/"
				}
			}
			_, _ = fmt.Fprintf(w, "<D:response><D:href>%s</D:href><D:propstat><D:prop><D:displayname>%s</D:displayname><D:resourcetype>%s</D:resourcetype><D:getcontentlength>%d</D:getcontentlength><D:getlastmodified>Thu, 01 Jan 1970 00:00:00 GMT</D:getlastmodified></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>", escape(href), escape(e.name), kind, e.size)
		}
		_, _ = io.WriteString(w, "</D:multistatus>")
		return
	}
	if entry.directory {
		http.Error(w, "directory listing requires PROPFIND", 405)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	if r.Method == "HEAD" {
		w.Header().Set("Content-Length", strconv.FormatInt(entry.size, 10))
		w.WriteHeader(200)
		return
	}
	if !entry.ready {
		http.Error(w, "provider placement not ready", 503)
		return
	}
	s.stream(w, r, entry)
}
func (s *Service) stream(w http.ResponseWriter, r *http.Request, e davEntry) {
	transport := streamHTTPClient
	for attempt := 0; attempt < 2; attempt++ {
		link, err := s.deliveryLink(r.Context(), e.protocol, e.provider, e.file, attempt > 0)
		if err != nil {
			http.Error(w, "cloud delivery link unavailable", 502)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), "GET", link, nil)
		if err != nil {
			http.Error(w, "invalid delivery link", 502)
			return
		}
		for _, header := range []string{"Range", "If-Range"} {
			if v := r.Header.Get(header); v != "" {
				req.Header.Set(header, v)
			}
		}
		resp, err := transport.Do(req)
		if err != nil {
			http.Error(w, "cloud delivery unavailable", 502)
			return
		}
		if (resp.StatusCode == 401 || resp.StatusCode == 403) && attempt == 0 {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != 200 && resp.StatusCode != 206 && resp.StatusCode != 416 {
			resp.Body.Close()
			http.Error(w, "cloud delivery failed", 502)
			return
		}
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		resp.Body.Close()
		return
	}
}
