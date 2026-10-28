// Package guiassets embeds the built Vue frontend as the Wails web root.
// The assets directory is populated by `make frontend-build`; a placeholder
// keeps the package buildable without a frontend toolchain.
package guiassets

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var embedded embed.FS

// Assets returns the web root filesystem for the Wails asset server.
func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}
