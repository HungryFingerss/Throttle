// Package web embeds the static dashboard assets so the daemon ships them in a
// single binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html style.css app.js
var assets embed.FS

// FS returns the dashboard asset filesystem (rooted so "/" serves index.html).
func FS() fs.FS { return assets }
