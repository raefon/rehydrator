package gateway

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/raefon/rehydrator/internal/db"
)

func releaseFolder(j db.GatewayJob) string {
	name := strings.TrimSpace(j.Name)
	if name == "" {
		name = j.Filename
	}
	for _, suffix := range []string{".nzb", ".torrent"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			name = name[:len(name)-len(suffix)]
			break
		}
	}
	var out strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			r = '-'
		}
		if out.Len()+len(string(r)) > 180 {
			break
		}
		out.WriteRune(r)
	}
	name = strings.Trim(out.String(), " .")
	if name == "" || name == "." || name == ".." {
		return "release"
	}
	return name
}
func validReleaseFolder(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}
