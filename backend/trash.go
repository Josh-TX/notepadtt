package backend

import (
	"database/sql"
	"strings"
	"time"
)

// FileTrash is a deleted file awaiting restore or permanent purge. Unlike FileVersion,
// there's no VersionId here — restoring always mints a fresh one via the normal
// content-write path, the same as any other write.
type FileTrash struct {
	FileId      string
	Path        string
	Content     string
	DateDeleted int64
	Size        int
}

// FileTrashSummary is one row of the Trash Modal's sidebar list: everything about a
// trashed file except its content, which is fetched separately and on demand.
type FileTrashSummary struct {
	FileId      string `json:"fileId"`
	Path        string `json:"path"`
	LineCount   int    `json:"lineCount"`
	Length      int    `json:"length"`
	DateDeleted int64  `json:"dateDeleted"`
}

func createFileTrashSchema(sqldb *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS FileTrash (
			Id INTEGER PRIMARY KEY AUTOINCREMENT,
			FileId TEXT,
			Path TEXT,
			Content TEXT,
			DateDeleted INTEGER,
			Size INTEGER
		)`,
	}
	for _, stmt := range stmts {
		if _, err := sqldb.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// TrashFile deletes a file's files row and, only if this call is the one that actually
// removed it, inserts a FileTrash row capturing its path/content as of deletion. Every
// deletion path (single-file, folder, watcher, startup scan) funnels through this, and
// disk removal (os.Remove/RemoveAll) always races against the watcher's own fsnotify
// Remove handler reaching the same fileId — the RowsAffected check is what keeps that
// race from producing a duplicate trash row.
func (d *DB) TrashFile(fileId, relPath, content string, dateDeleted int64) error {
	res, err := d.sql.Exec(`DELETE FROM files WHERE FileId=?`, fileId)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	_, err = d.sql.Exec(`INSERT INTO FileTrash (FileId, Path, Content, DateDeleted, Size) VALUES (?,?,?,?,?)`,
		fileId, relPath, content, dateDeleted, len(content))
	return err
}

// GetTrashList returns every trashed file as a summary (no content), most-recently-
// deleted first.
func (d *DB) GetTrashList() ([]FileTrashSummary, error) {
	rows, err := d.sql.Query(`SELECT FileId, Path, Content, DateDeleted, Size FROM FileTrash ORDER BY DateDeleted DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileTrashSummary{}
	for rows.Next() {
		var fileId, path, content string
		var dateDeleted int64
		var size int
		if err := rows.Scan(&fileId, &path, &content, &dateDeleted, &size); err != nil {
			return nil, err
		}
		out = append(out, FileTrashSummary{
			FileId:      fileId,
			Path:        path,
			LineCount:   strings.Count(content, "\n") + 1,
			Length:      size,
			DateDeleted: dateDeleted,
		})
	}
	return out, rows.Err()
}

// GetFileTrash returns one trashed file's full record (including content), used for
// restore and for the Trash Modal's on-demand content fetch. found is false if no
// FileTrash row exists for fileId.
func (d *DB) GetFileTrash(fileId string) (t FileTrash, found bool, err error) {
	err = d.sql.QueryRow(`SELECT FileId, Path, Content, DateDeleted, Size FROM FileTrash WHERE FileId=?`, fileId).
		Scan(&t.FileId, &t.Path, &t.Content, &t.DateDeleted, &t.Size)
	if err == sql.ErrNoRows {
		return FileTrash{}, false, nil
	}
	if err != nil {
		return FileTrash{}, false, err
	}
	return t, true, nil
}

// DeleteFileTrash permanently removes one FileTrash row (restore success or the Trash
// Modal's "Delete Forever" action).
func (d *DB) DeleteFileTrash(fileId string) error {
	_, err := d.sql.Exec(`DELETE FROM FileTrash WHERE FileId=?`, fileId)
	return err
}

// DeleteAllFileTrash empties the trash (the Trash Modal's "Empty Trash" action).
func (d *DB) DeleteAllFileTrash() error {
	_, err := d.sql.Exec(`DELETE FROM FileTrash`)
	return err
}

// DeleteExpiredFileTrash hard-deletes trash rows older than TrashTTL.
func (d *DB) DeleteExpiredFileTrash(now time.Time) error {
	_, err := d.sql.Exec(`DELETE FROM FileTrash WHERE DateDeleted < ?`, now.UnixMilli()-TrashTTL.Milliseconds())
	return err
}
