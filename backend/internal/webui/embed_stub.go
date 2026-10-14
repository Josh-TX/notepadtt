//go:build !embed_frontend

package webui

import "embed"

// Built without -tags embed_frontend: no UI is served (use the Vite dev server).
var distFS embed.FS
