// Package version holds the build version string.
// The default "dev" is used when running via `go run` or building without
// ldflags. It is overridden at build time via
// -ldflags "-X auto-router/internal/version.Version=v...".
package version

// Version is the application version. Defaults to "dev" for local builds.
var Version = "dev"
