package backend

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

// FileVersion is a single immutable historical content snapshot. Rows are inserted
// once by the FileVersioning process, never updated, and eventually hard-deleted by
// the TTL sweep once they age past their own Term's TTL.
type FileVersion struct {
	FileId    string
	Path      string // the file's path at the time this version was current
	Content   string
	VersionId string
	Date      int64 // unix millis; the moment this version became non-current
	Term      int   // retention tier, 1-4, cumulative (1=short, 2=+med, 3=+long, 4=+verylong)
}

func createFileVersionsSchema(sqldb *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS FileVersions (
			Id INTEGER PRIMARY KEY AUTOINCREMENT,
			FileId TEXT,
			Path TEXT,
			Content TEXT,
			VersionId TEXT,
			Date INTEGER,
			Term INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fileversions_fileid_term_date ON FileVersions(FileId, Term, Date)`,
		`CREATE INDEX IF NOT EXISTS idx_fileversions_term_date ON FileVersions(Term, Date)`,
	}
	for _, stmt := range stmts {
		if _, err := sqldb.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// InsertFileVersion persists a single historical snapshot.
func (d *DB) InsertFileVersion(fv FileVersion) error {
	_, err := d.sql.Exec(`INSERT INTO FileVersions (FileId, Path, Content, VersionId, Date, Term) VALUES (?,?,?,?,?,?)`,
		fv.FileId, fv.Path, fv.Content, fv.VersionId, fv.Date, fv.Term)
	return err
}

// GetFileVersionsByFileId returns every persisted version for one FileId, unordered;
// callers sort as needed.
func (d *DB) GetFileVersionsByFileId(fileId string) ([]FileVersion, error) {
	rows, err := d.sql.Query(`SELECT FileId, Path, Content, VersionId, Date, Term FROM FileVersions WHERE FileId=?`, fileId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileVersion
	for rows.Next() {
		var fv FileVersion
		if err := rows.Scan(&fv.FileId, &fv.Path, &fv.Content, &fv.VersionId, &fv.Date, &fv.Term); err != nil {
			return nil, err
		}
		out = append(out, fv)
	}
	return out, rows.Err()
}

// FileVersionResponse is one element of GET /api/files/{id}/versions. Pending marks
// previews computed on demand from RecentFileVersions that would become a real
// FileVersions row the next time the FileVersioning process runs, but haven't yet.
// Current marks the single synthetic entry (always element 0, when present) carrying
// the file's live DB content, sparing callers a second request just to peek at it.
// Trashed marks that this Current entry is actually sourced from a FileTrash row
// rather than a live files row — see currentOrTrashedSnapshotResponse.
type FileVersionResponse struct {
	FileId    string `json:"fileId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	VersionId string `json:"versionId"`
	Date      int64  `json:"date"`
	Term      int    `json:"term"`
	Pending   bool   `json:"pending"`
	Current   bool   `json:"current"`
	Trashed   bool   `json:"trashed"`
}

// currentSnapshotResponse builds the synthetic current-content entry. VersionId,
// Date, and Term are zero-value sentinels — this isn't a real historical version.
func currentSnapshotResponse(f *DBFile) FileVersionResponse {
	return FileVersionResponse{
		FileId:  f.FileId,
		Path:    f.Path,
		Content: f.Content,
		Current: true,
	}
}

// currentOrTrashedSnapshotResponse builds HistoryModal's pinned top-of-sidebar entry.
// If the file still exists live, it's the usual "Current Snapshot" sourced from f. If
// the file has been deleted but a FileTrash row for it still exists, it's "Trashed
// Content" sourced from trash instead — the closest thing to "current" a fully-deleted
// file still has. If neither exists (the file is gone and its trash row has since
// expired/been purged too), found is false and callers omit the entry entirely.
func currentOrTrashedSnapshotResponse(f *DBFile, trash *FileTrash) (resp FileVersionResponse, found bool) {
	if f != nil {
		return currentSnapshotResponse(f), true
	}
	if trash != nil {
		return FileVersionResponse{
			FileId:  trash.FileId,
			Path:    trash.Path,
			Content: trash.Content,
			Current: true,
			Trashed: true,
		}, true
	}
	return FileVersionResponse{}, false
}

// buildFileVersionResponses merges persisted rows (pending:false) and on-demand
// eligibility-walk previews (pending:true) into one newest-first-by-Date list.
// Always returns a non-nil slice, even when both inputs are empty.
func buildFileVersionResponses(persisted, preview []FileVersion) []FileVersionResponse {
	out := make([]FileVersionResponse, 0, len(persisted)+len(preview))
	for _, fv := range persisted {
		out = append(out, fileVersionResponse(fv, false))
	}
	for _, fv := range preview {
		out = append(out, fileVersionResponse(fv, true))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func fileVersionResponse(fv FileVersion, pending bool) FileVersionResponse {
	return FileVersionResponse{
		FileId:    fv.FileId,
		Path:      fv.Path,
		Content:   fv.Content,
		VersionId: fv.VersionId,
		Date:      fv.Date,
		Term:      fv.Term,
		Pending:   pending,
	}
}

// DeleteExpiredFileVersions hard-deletes rows that have aged past their own Term's TTL.
func (d *DB) DeleteExpiredFileVersions(now time.Time) error {
	nowMillis := now.UnixMilli()
	_, err := d.sql.Exec(`DELETE FROM FileVersions WHERE
		(Term=1 AND Date < ?) OR
		(Term=2 AND Date < ?) OR
		(Term=3 AND Date < ?) OR
		(Term=4 AND Date < ?)`,
		nowMillis-ShortTermTTL.Milliseconds(),
		nowMillis-MedTermTTL.Milliseconds(),
		nowMillis-LongTermTTL.Milliseconds(),
		nowMillis-VeryLongTermTTL.Milliseconds(),
	)
	return err
}

// lastVersionDates holds, per cumulative term threshold N (index N-1, N in 1..4), the
// most recent Date among existing FileVersions rows satisfying Term>=N for one FileId.
// A zero value means no qualifying row exists yet for that threshold.
type lastVersionDates [4]int64

// GetLastVersionDates runs one grouped query across fileIds, returning for each the
// most recent Date at every cumulative term threshold (MAX(Date) WHERE Term>=N, for N
// in 1..4), since Term values are cumulative (a Term=3 row counts toward >=1, >=2, >=3).
func (d *DB) GetLastVersionDates(fileIds []string) (map[string]lastVersionDates, error) {
	result := map[string]lastVersionDates{}
	if len(fileIds) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(fileIds))
	args := make([]interface{}, len(fileIds))
	for i, id := range fileIds {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT FileId,
		MAX(CASE WHEN Term>=1 THEN Date END),
		MAX(CASE WHEN Term>=2 THEN Date END),
		MAX(CASE WHEN Term>=3 THEN Date END),
		MAX(CASE WHEN Term>=4 THEN Date END)
		FROM FileVersions WHERE FileId IN (` + strings.Join(placeholders, ",") + `) GROUP BY FileId`
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fileId string
		var t1, t2, t3, t4 sql.NullInt64
		if err := rows.Scan(&fileId, &t1, &t2, &t3, &t4); err != nil {
			continue
		}
		result[fileId] = lastVersionDates{t1.Int64, t2.Int64, t3.Int64, t4.Int64}
	}
	return result, nil
}
