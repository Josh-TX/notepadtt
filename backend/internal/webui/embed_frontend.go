//go:build embed_frontend

package webui

import "embed"

//go:embed all:dist
var distFS embed.FS
