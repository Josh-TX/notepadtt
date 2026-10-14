// Package webui exposes the built Vue frontend (frontend/ builds into dist/ here).
// It is only embedded when built with -tags embed_frontend.
package webui

import "io/fs"

// FS returns the embedded frontend build rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return distFS
	}
	return sub
}

// Embedded reports whether a frontend build is embedded.
func Embedded() bool {
	_, err := fs.Stat(distFS, "dist/index.html")
	return err == nil
}
