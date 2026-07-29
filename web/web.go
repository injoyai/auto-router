// Package web embeds the built React SPA located in web/dist.
// The embed directive lives in this package (not cmd/router) because
// //go:embed paths are resolved relative to the source file's directory,
// so `dist` here correctly points to web/dist.
package web

import "embed"

// FS is the embedded filesystem rooted at web/dist. Use fs.Sub(FS, "dist")
// to obtain a filesystem whose root is the dist directory.
//
//go:embed all:dist
var FS embed.FS
