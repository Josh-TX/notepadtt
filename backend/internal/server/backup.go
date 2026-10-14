package server

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// flushAll writes every dirty open file to disk.
func (s *Server) flushAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.byPath {
		s.flushLocked(f)
	}
}

// handleBackupSize reports the uncompressed size of everything a backup would contain.
func (s *Server) handleBackupSize(w http.ResponseWriter, r *http.Request) {
	s.flushAll()
	var files, bytes int64
	walkFollowSymlinks(s.rootAbs, nil, func(p string) error {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			files++
			bytes += info.Size()
		}
		return nil
	})
	writeJSON(w, map[string]int64{"files": files, "bytes": bytes})
}

// handleBackup streams the data root as a zip.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	s.flushAll()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q",
		"backup-"+time.Now().Format("2006-01-02-150405")+".zip"))
	zw := zip.NewWriter(w)
	defer zw.Close()
	walkFollowSymlinks(s.rootAbs, nil, func(p string) error {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(s.rootAbs, p)
		if err != nil {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer in.Close()
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		return err
	})
}
