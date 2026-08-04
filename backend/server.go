package backend

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

type Server struct {
	root string
	db   *DB
	hub  *Hub
	mux  *http.ServeMux
}

func NewServer(root string, frontend embed.FS) (*Server, error) {
	db, err := NewDB(root)
	if err != nil {
		return nil, err
	}
	hub := NewHub()
	StartFileVersioning(db, hub.Versions)
	s := &Server{root: root, db: db, hub: hub, mux: http.NewServeMux()}
	hub.EditHandler = s.HandleEdit
	s.registerRoutes(frontend)
	if err := StartWatcher(root, db, hub); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes(frontend embed.FS) {
	// WebSocket
	s.mux.HandleFunc("GET /ws", s.hub.ServeWS)

	// REST API
	s.mux.HandleFunc("GET /api/files", s.handleGetFiles)
	s.mux.HandleFunc("GET /api/files/{id}", s.handleGetFile)
	s.mux.HandleFunc("GET /api/files/{id}/versions", s.handleGetFileVersions)
	s.mux.HandleFunc("POST /api/files", s.handleCreateFile)
	s.mux.HandleFunc("PUT /api/files/{id}", s.handleRenameFile)
	s.mux.HandleFunc("DELETE /api/files/{id}", s.handleDeleteFile)
	s.mux.HandleFunc("POST /api/files/{id}/duplicate", s.handleDuplicateFile)
	s.mux.HandleFunc("PUT /api/files/{id}/order", s.handleReorderFile)
	s.mux.HandleFunc("PUT /api/files/{id}/move", s.handleMoveFile)
	s.mux.HandleFunc("POST /api/folders", s.handleCreateFolder)
	s.mux.HandleFunc("PUT /api/folders", s.handleRenameFolder)
	s.mux.HandleFunc("DELETE /api/folders", s.handleDeleteFolder)
	s.mux.HandleFunc("PUT /api/folders/move", s.handleMoveFolder)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/trash", s.handleListTrash)
	s.mux.HandleFunc("GET /api/trash/{id}", s.handleGetTrashContent)
	s.mux.HandleFunc("POST /api/trash/{id}/restore", s.handleRestoreFile)
	s.mux.HandleFunc("DELETE /api/trash/{id}", s.handleDeleteTrashItem)
	s.mux.HandleFunc("DELETE /api/trash", s.handleEmptyTrash)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	s.mux.HandleFunc("PUT /api/settings/wordwrap", s.handleUpdateWrap)
	s.mux.HandleFunc("PUT /api/settings/sidebarwidth", s.handleUpdateSidebarWidth)
	s.mux.HandleFunc("PUT /api/settings/desktopsidebaropen", s.handleUpdateDesktopSidebarOpen)
	s.mux.HandleFunc("POST /api/scan", s.handleScanFiles)

	// Static frontend
	sub, _ := fs.Sub(frontend, "frontend/dist")
	fileServer := http.FileServer(http.FS(sub))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// For hash routing, non-asset requests serve index.html
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" || !strings.Contains(path, ".") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
