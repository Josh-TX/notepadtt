package backend

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	trashDirName    = ".ntt-trash"
	trashTTL        = 5 * time.Second
	maxDisplayBytes = 1000 * 1024
)

type Server struct {
	root    string // as given
	rootAbs string
	hub     *Hub
	mux     *http.ServeMux

	// mu guards everything below. It is held across disk writes of open files so
	// the watcher (which also takes it before re-reading disk) never observes a
	// half-written file.
	mu         sync.Mutex
	byId       map[string]*openFile
	byPath     map[string]*openFile
	tabs       []string // fileIds, in tab order
	lastActive string
	trash      map[string]*trashItem

	treeMu    sync.Mutex
	treeTimer *time.Timer
	treeFirst time.Time
}

func NewServer(root string, frontend embed.FS) (*Server, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Crash recovery: anything left in the trash from a previous run is gone for good.
	os.RemoveAll(filepath.Join(rootAbs, trashDirName))

	s := &Server{
		root:    root,
		rootAbs: rootAbs,
		hub:     NewHub(),
		mux:     http.NewServeMux(),
		byId:    map[string]*openFile{},
		byPath:  map[string]*openFile{},
		trash:   map[string]*trashItem{},
	}
	s.hub.EditHandler = s.HandleEdit
	s.hub.OnUnsubscribe = func(fileId string) {
		s.mu.Lock()
		s.maybeUnloadLocked(fileId)
		s.mu.Unlock()
	}
	s.registerRoutes(frontend)
	if err := s.startWatcher(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CSRF guard: state-changing requests from a browser page on another origin
	// carry a mismatching Origin header.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes(frontend embed.FS) {
	s.mux.HandleFunc("GET /ws", s.hub.ServeWS)

	s.mux.HandleFunc("GET /api/tree", s.handleGetTree)
	s.mux.HandleFunc("GET /api/tabs", s.handleGetTabs)
	s.mux.HandleFunc("POST /api/tabs", s.handleOpenTab)
	s.mux.HandleFunc("PUT /api/tabs/order", s.handleReorderTabs)
	s.mux.HandleFunc("DELETE /api/tabs/{id}", s.handleCloseTab)
	s.mux.HandleFunc("GET /api/files/{id}", s.handleGetFile)
	s.mux.HandleFunc("GET /api/download", s.handleDownload)

	s.mux.HandleFunc("POST /api/files", s.handleCreateFile)
	s.mux.HandleFunc("POST /api/folders", s.handleCreateFolder)
	s.mux.HandleFunc("POST /api/duplicate", s.handleDuplicate)
	s.mux.HandleFunc("PUT /api/rename", s.handleRename)
	s.mux.HandleFunc("PUT /api/move", s.handleMove)
	s.mux.HandleFunc("DELETE /api/entries", s.handleDelete)
	s.mux.HandleFunc("POST /api/trash/{id}/restore", s.handleRestore)

	s.mux.HandleFunc("GET /api/search", s.handleSearch)

	sub, _ := fs.Sub(frontend, "frontend/dist")
	fileServer := http.FileServer(http.FS(sub))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
