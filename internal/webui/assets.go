// Package webui serves the frontend compiled by npm run build in web/.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var assets embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(root))
}
