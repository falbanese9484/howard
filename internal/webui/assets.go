package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var assetsFS embed.FS

// staticFS is the embedded frontend rooted at the "static" directory so files
// are served from the web root (e.g. "app.js" rather than "static/app.js").
func staticFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "static")
	if err != nil {
		// The embed directive guarantees "static" exists at build time, so this
		// is unreachable in a correctly built binary.
		panic(err)
	}
	return sub
}
