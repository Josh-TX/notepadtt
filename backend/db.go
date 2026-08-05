package backend

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var allowedExtensions = map[string]bool{
	".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
	".sh": true, ".html": true, ".css": true, ".js": true, ".xml": true,
	".csv": true, ".py": true, ".ts": true, ".java": true, ".c": true,
	".cpp": true, ".toml": true, ".ini": true, ".log": true, ".sql": true,
	".rs": true, ".go": true, ".php": true, ".rb": true, ".swift": true,
	".kt": true, ".vue": true, ".jsx": true, ".tsx": true, ".env": true,
	".conf": true,
}

var newNPattern = regexp.MustCompile(`^new \d+$`)

// textExtensionsList is the sorted allowedExtensions keys, exposed to the frontend via
// the settings API so it can predict IsAllowedPath's outcome without a round-trip
// (e.g. to warn before a rename that would untrack a file).
var textExtensionsList = func() []string {
	list := make([]string, 0, len(allowedExtensions))
	for ext := range allowedExtensions {
		list = append(list, ext)
	}
	sort.Strings(list)
	return list
}()

type DBFile struct {
	FileId     string
	Path       string
	Content    string
	LastOpened int64 // unix millis, 0 = never
	OrderNum   int
	VersionId  string
}

type DB struct {
	sql  *sql.DB
	root string
}

func NewDB(root string) (*DB, error) {
	dbPath := filepath.Join(root, ".notepadtt.db")
	dsn := dbPath + "?_pragma=busy_timeout(5000)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)

	_, err = sqldb.Exec(`CREATE TABLE IF NOT EXISTS files (
		Id INTEGER PRIMARY KEY AUTOINCREMENT,
		FileId TEXT UNIQUE,
		Path TEXT UNIQUE,
		Content TEXT,
		LastOpened INTEGER,
		ContentUpdated INTEGER,
		OrderNum INTEGER,
		VersionId TEXT
	)`)
	if err != nil {
		return nil, err
	}

	if err := createFileVersionsSchema(sqldb); err != nil {
		return nil, err
	}

	if err := createFileTrashSchema(sqldb); err != nil {
		return nil, err
	}

	if err := createSettingsSchema(sqldb); err != nil {
		return nil, err
	}

	db := &DB{sql: sqldb, root: root}
	settings, err := db.GetSettings()
	if err != nil {
		return nil, err
	}
	if err := setSettingsCache(settings); err != nil {
		return nil, err
	}
	if err := db.Scan(); err != nil {
		return nil, err
	}
	return db, nil
}

// setWAL switches journal mode between WAL (fast, fsync-free commits, but leaves -wal/-shm
// files alongside the main db file) and the default DELETE rollback-journal mode (fsyncs
// every commit, but only ever a single db file on disk between writes).
func (d *DB) setWAL(enable bool) error {
	mode, sync := "DELETE", "FULL"
	if enable {
		mode, sync = "WAL", "NORMAL"
	}
	if _, err := d.sql.Exec(fmt.Sprintf("PRAGMA journal_mode=%s", mode)); err != nil {
		return err
	}
	if _, err := d.sql.Exec(fmt.Sprintf("PRAGMA synchronous=%s", sync)); err != nil {
		return err
	}
	return nil
}

// IsAllowedPath reports whether relPath should be tracked, honoring the live
// onlyTextExt setting: when false, every path is allowed; when true, only paths
// matching isTextExtension are.
func (d *DB) IsAllowedPath(relPath string) bool {
	if !GetSettingsCache().OnlyTextExt {
		return true
	}
	return isTextExtension(relPath)
}

// isTextExtension reports whether relPath's name matches a tracked-by-default
// extension or the "new N" auto-created-file pattern, independent of the
// onlyTextExt setting. Used both by IsAllowedPath and by CleanupNonTextExtension
// (which only ever runs while onlyTextExt is true, but checks the raw rule
// directly rather than going through the setting-aware wrapper).
func isTextExtension(relPath string) bool {
	name := filepath.Base(relPath)
	if newNPattern.MatchString(name) {
		return true
	}
	return allowedExtensions[filepath.Ext(name)]
}

func (d *DB) IsTracked(relPath string) bool {
	var count int
	d.sql.QueryRow(`SELECT COUNT(*) FROM files WHERE Path = ?`, relPath).Scan(&count)
	return count > 0
}

// withinMaxFileSize reports whether size (bytes) is within the live MaxFileSizeKB
// setting (1 KB = 1000 bytes, per the setting's own unit).
func withinMaxFileSize(size int64) bool {
	return size <= int64(GetSettingsCache().MaxFileSizeKB)*1000
}

// Scan reconciles the DB against disk: tracks new allowed files within the size limit,
// updates content that changed on disk while the app wasn't running, trashes DB entries
// for files removed from disk, purges tracked files that grew past MaxFileSizeKB, and —
// when onlyTextExt is enabled — purges any tracked/trashed file whose path now has a
// disallowed extension (see CleanupNonTextExtension). Purged files aren't recoverable via
// FileTrash, unlike a disk deletion. Runs once at startup; also the basis for a future
// on-demand "scan" trigger.
func (d *DB) Scan() error {
	start := time.Now()
	walked, updated, trashed, inserted, cleaned := 0, 0, 0, 0, 0
	// Bulk inserts/deletes below can number in the hundreds; the default rollback-journal
	// mode fsyncs (twice) per statement, so temporarily switch to WAL (fsync-free commits)
	// for the duration of the scan, then switch back so the on-disk footprint returns to
	// a single .notepadtt.db file (switching away from WAL checkpoints and removes the
	// -wal/-shm files).
	if err := d.setWAL(true); err != nil {
		log.Printf("scan: failed to enable WAL: %v", err)
	}
	defer func() {
		if err := d.setWAL(false); err != nil {
			log.Printf("scan: failed to disable WAL: %v", err)
		}
		// cleaned only counts purgeFileId calls (disallowed extension / oversized), the
		// only paths that fully delete rows rather than moving them to FileTrash — the
		// only cases where freed pages are worth reclaiming via VACUUM (which rewrites
		// the whole file, so skip it otherwise).
		if cleaned > 0 {
			if _, err := d.sql.Exec("VACUUM"); err != nil {
				log.Printf("scan: vacuum failed: %v", err)
			}
		}
		log.Printf("scan: done in %s (walked=%d updated=%d trashed=%d inserted=%d cleaned=%d)",
			time.Since(start), walked, updated, trashed, inserted, cleaned)
	}()

	// load existing DB records
	rows, err := d.sql.Query(`SELECT FileId, Path, Content, ContentUpdated, VersionId FROM files`)
	if err != nil {
		return err
	}
	type dbRec struct {
		fileId         string
		content        string
		contentUpdated int64
		versionId      string
	}
	dbByPath := map[string]dbRec{}
	for rows.Next() {
		var r dbRec
		var path string
		rows.Scan(&r.fileId, &path, &r.content, &r.contentUpdated, &r.versionId)
		dbByPath[path] = r
	}
	rows.Close()

	diskPaths := map[string]bool{}

	type pendingInsert struct {
		relPath string
		content string
	}
	var pending []pendingInsert

	err = walkFollowSymlinks(d.root, nil, func(path string) error {
		rel, _ := filepath.Rel(d.root, path)
		rel = filepath.ToSlash(rel)

		rec, inDB := dbByPath[rel]
		if !inDB && !d.IsAllowedPath(rel) {
			return nil
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil
		}

		if !withinMaxFileSize(info.Size()) {
			// Mark as seen even though it's excluded, so the "no longer on disk" pass
			// below doesn't also try (harmlessly, but redundantly) to trash it.
			diskPaths[rel] = true
			if inDB {
				// was tracked but has grown past MaxFileSizeKB (or the setting was
				// lowered) since the last scan: purge it, same as a disallowed
				// extension — not recoverable via FileTrash, since the exclusion
				// rules are policy-driven exclusions rather than a disk deletion.
				d.purgeFileId(rec.fileId)
				cleaned++
				log.Printf("scan: purged oversized file %s", rel)
			}
			return nil
		}

		diskPaths[rel] = true
		walked++
		diskMtime := info.ModTime().UnixMilli()

		b, _ := os.ReadFile(path)
		content := string(b)

		if !inDB {
			pending = append(pending, pendingInsert{rel, content})
			return nil
		}

		if diskMtime > rec.contentUpdated {
			now := time.Now().UnixMilli()
			d.sql.Exec(`UPDATE files SET Content=?, ContentUpdated=?, VersionId=? WHERE FileId=?`,
				content, now, uniqueId(5), rec.fileId)
			updated++
			log.Printf("startup: updated %s from disk (disk newer)", rel)
		} else if rec.versionId == "" {
			d.sql.Exec(`UPDATE files SET VersionId=? WHERE FileId=?`, uniqueId(5), rec.fileId)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Trash DB entries for files no longer on disk (e.g. deleted while the app wasn't running).
	now := time.Now().UnixMilli()
	for path, rec := range dbByPath {
		if !diskPaths[path] {
			d.TrashFile(rec.fileId, path, rec.content, now)
			trashed++
			log.Printf("startup: trashed stale DB entry for %s", path)
		}
	}

	// Group pending inserts by folder and assign OrderNums alphabetically within each folder.
	byFolder := map[string][]pendingInsert{}
	for _, p := range pending {
		folder := folderOf(p.relPath)
		byFolder[folder] = append(byFolder[folder], p)
	}
	for folder, files := range byFolder {
		sort.Slice(files, func(i, j int) bool {
			return strings.ToLower(filepath.Base(files[i].relPath)) < strings.ToLower(filepath.Base(files[j].relPath))
		})
		startOrder := d.GetMaxOrderNumInFolder(folder) + 1
		for i, p := range files {
			d.insertFileRecord(p.relPath, p.content, startOrder+i)
			inserted++
		}
	}

	if GetSettingsCache().OnlyTextExt {
		n, err := d.CleanupNonTextExtension("")
		if err != nil {
			return err
		}
		cleaned += n
	}

	return nil
}

// CleanupNonTextExtension purges DB rows across Files, FileVersions, and FileTrash for
// any FileId whose current (Files.Path if tracked, else FileTrash.Path if trashed) path
// has a disallowed extension. Disk files are never touched. If scopeFileId is non-empty,
// only that FileId is checked (used right after a rename); otherwise every FileId
// referenced by Files or FileTrash is scanned. FileIds with no Files or FileTrash row
// (orphaned FileVersions only) are left alone.
func (d *DB) CleanupNonTextExtension(scopeFileId string) (int, error) {
	fileIds := map[string]bool{}
	if scopeFileId != "" {
		fileIds[scopeFileId] = true
	} else {
		for _, table := range []string{"files", "FileTrash"} {
			rows, err := d.sql.Query(fmt.Sprintf(`SELECT DISTINCT FileId FROM %s`, table))
			if err != nil {
				return 0, err
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return 0, err
				}
				fileIds[id] = true
			}
			rows.Close()
		}
	}

	purged := 0
	for fileId := range fileIds {
		path, ok := d.currentOrTrashedPath(fileId)
		if !ok || isTextExtension(path) {
			continue
		}
		if err := d.purgeFileId(fileId); err != nil {
			return purged, err
		}
		purged++
		log.Printf("cleanup: purged non-text-extension records for %s", path)
	}
	return purged, nil
}

// purgeFileId hard-deletes every row referencing fileId across Files, FileTrash, and
// FileVersions. Disk files are never touched. Used for exclusion rules (disallowed
// extension, oversized) where the file shouldn't be recoverable via FileTrash/HistoryModal
// once it no longer qualifies for tracking — unlike an ordinary disk deletion, which still
// goes through TrashFile so it stays recoverable.
func (d *DB) purgeFileId(fileId string) error {
	if _, err := d.sql.Exec(`DELETE FROM files WHERE FileId=?`, fileId); err != nil {
		return err
	}
	if _, err := d.sql.Exec(`DELETE FROM FileTrash WHERE FileId=?`, fileId); err != nil {
		return err
	}
	if _, err := d.sql.Exec(`DELETE FROM FileVersions WHERE FileId=?`, fileId); err != nil {
		return err
	}
	return nil
}

// currentOrTrashedPath returns a FileId's current Files.Path if tracked, else its
// FileTrash.Path if trashed, else ok=false.
func (d *DB) currentOrTrashedPath(fileId string) (path string, ok bool) {
	if err := d.sql.QueryRow(`SELECT Path FROM files WHERE FileId=?`, fileId).Scan(&path); err == nil {
		return path, true
	}
	if err := d.sql.QueryRow(`SELECT Path FROM FileTrash WHERE FileId=?`, fileId).Scan(&path); err == nil {
		return path, true
	}
	return "", false
}

func (d *DB) insertFileRecord(relPath, content string, orderNum int) (string, error) {
	id := uniqueId(12)
	versionId := uniqueId(5)
	now := time.Now().UnixMilli()
	_, err := d.sql.Exec(`INSERT INTO files (FileId, Path, Content, LastOpened, ContentUpdated, OrderNum, VersionId) VALUES (?,?,?,?,?,?,?)`,
		id, relPath, content, 0, now, orderNum, versionId)
	if err != nil {
		return "", err
	}
	return id, nil
}

// InsertFileWithOrder inserts a new file record with an explicit OrderNum.
// Use this when the caller controls the OrderNum (e.g. duplicate).
func (d *DB) InsertFileWithOrder(relPath, content string, orderNum int) (string, error) {
	return d.insertFileRecord(relPath, content, orderNum)
}

// InsertFileWithId inserts a files row reusing an existing FileId (trash restore)
// rather than minting a new one, so its prior FileVersion history reattaches.
// Returns the fresh VersionId assigned to this write.
func (d *DB) InsertFileWithId(fileId, relPath, content string, orderNum int) (string, error) {
	versionId := uniqueId(5)
	now := time.Now().UnixMilli()
	_, err := d.sql.Exec(`INSERT INTO files (FileId, Path, Content, LastOpened, ContentUpdated, OrderNum, VersionId) VALUES (?,?,?,?,?,?,?)`,
		fileId, relPath, content, 0, now, orderNum, versionId)
	if err != nil {
		return "", err
	}
	return versionId, nil
}

func (d *DB) GetAllFiles() ([]DBFile, error) {
	rows, err := d.sql.Query(`SELECT FileId, Path, LastOpened, OrderNum FROM files ORDER BY Path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []DBFile
	for rows.Next() {
		var f DBFile
		rows.Scan(&f.FileId, &f.Path, &f.LastOpened, &f.OrderNum)
		files = append(files, f)
	}
	return files, nil
}

// GetAllFilesOnDisk returns DB files that actually exist on disk.
func (d *DB) GetAllFilesOnDisk() ([]DBFile, error) {
	all, err := d.GetAllFiles()
	if err != nil {
		return nil, err
	}
	var result []DBFile
	for _, f := range all {
		if _, err := os.Stat(filepath.Join(d.root, filepath.FromSlash(f.Path))); err == nil {
			result = append(result, f)
		}
	}
	return result, nil
}

func (d *DB) GetFile(fileId string) (*DBFile, error) {
	var f DBFile
	err := d.sql.QueryRow(`SELECT FileId, Path, Content, LastOpened, OrderNum, VersionId FROM files WHERE FileId=?`, fileId).
		Scan(&f.FileId, &f.Path, &f.Content, &f.LastOpened, &f.OrderNum, &f.VersionId)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}

func (d *DB) GetFileByPath(relPath string) (*DBFile, error) {
	var f DBFile
	err := d.sql.QueryRow(`SELECT FileId, Path, Content, LastOpened, OrderNum, VersionId FROM files WHERE Path=?`, relPath).
		Scan(&f.FileId, &f.Path, &f.Content, &f.LastOpened, &f.OrderNum, &f.VersionId)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}

func (d *DB) UpdateLastOpened(fileId string) error {
	_, err := d.sql.Exec(`UPDATE files SET LastOpened=? WHERE FileId=?`, time.Now().UnixMilli(), fileId)
	return err
}

func (d *DB) UpdateContent(fileId, content string) error {
	now := time.Now().UnixMilli()
	_, err := d.sql.Exec(`UPDATE files SET Content=?, ContentUpdated=? WHERE FileId=?`, content, now, fileId)
	return err
}

func (d *DB) UpdateContentAndVersion(fileId, content, versionId string) error {
	now := time.Now().UnixMilli()
	_, err := d.sql.Exec(`UPDATE files SET Content=?, ContentUpdated=?, VersionId=? WHERE FileId=?`,
		content, now, versionId, fileId)
	return err
}

// UpdateContentAndVersionIf updates content only when the stored VersionId matches
// expectedVersionId. Returns (true, nil) on success, (false, nil) if the version
// didn't match (concurrent write raced ahead), or (false, err) on DB error.
func (d *DB) UpdateContentAndVersionIf(fileId, content, versionId, expectedVersionId string) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := d.sql.Exec(`UPDATE files SET Content=?, ContentUpdated=?, VersionId=? WHERE FileId=? AND VersionId=?`,
		content, now, versionId, fileId, expectedVersionId)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (d *DB) UpdatePathById(fileId, newPath string) error {
	_, err := d.sql.Exec(`UPDATE files SET Path=? WHERE FileId=?`, newPath, fileId)
	return err
}

func (d *DB) UpdatePath(oldPath, newPath string) error {
	_, err := d.sql.Exec(`UPDATE files SET Path=? WHERE Path=?`, newPath, oldPath)
	return err
}

func (d *DB) UpdateFolderPath(oldPrefix, newPrefix string) error {
	rows, err := d.sql.Query(`SELECT FileId, Path FROM files WHERE Path=? OR Path LIKE ?`,
		oldPrefix, oldPrefix+"/%")
	if err != nil {
		return err
	}
	type rec struct{ id, path string }
	var recs []rec
	for rows.Next() {
		var r rec
		rows.Scan(&r.id, &r.path)
		recs = append(recs, r)
	}
	rows.Close()
	for _, r := range recs {
		newPath := newPrefix + strings.TrimPrefix(r.path, oldPrefix)
		d.sql.Exec(`UPDATE files SET Path=? WHERE FileId=?`, newPath, r.id)
	}
	return nil
}

func (d *DB) DeleteFile(fileId string) error {
	_, err := d.sql.Exec(`DELETE FROM files WHERE FileId=?`, fileId)
	return err
}

// GetFilesInFolderRecursive returns every file at or under folderPath (including
// nested subfolders), full rows incl. Content — used to trash a folder's contents
// before the recursive disk/DB delete.
func (d *DB) GetFilesInFolderRecursive(folderPath string) ([]DBFile, error) {
	rows, err := d.sql.Query(`SELECT FileId, Path, Content, LastOpened, OrderNum, VersionId FROM files WHERE Path=? OR Path LIKE ?`,
		folderPath, folderPath+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []DBFile
	for rows.Next() {
		var f DBFile
		if err := rows.Scan(&f.FileId, &f.Path, &f.Content, &f.LastOpened, &f.OrderNum, &f.VersionId); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetFolderTrackedStats returns the count and total content size (bytes) of every
// tracked file at or under folderPath — used to preview what a folder delete would
// move to trash.
func (d *DB) GetFolderTrackedStats(folderPath string) (count int, size int64, err error) {
	var totalSize sql.NullInt64
	row := d.sql.QueryRow(`SELECT COUNT(*), SUM(LENGTH(Content)) FROM files WHERE Path=? OR Path LIKE ?`,
		folderPath, folderPath+"/%")
	if err = row.Scan(&count, &totalSize); err != nil {
		return 0, 0, err
	}
	return count, totalSize.Int64, nil
}

// EnsureFileTracked tracks relPath if it isn't already, returning its FileId. Oversized
// files (over MaxFileSizeKB) are left untracked entirely and "" is returned with a nil
// error, mirroring how disallowed extensions are silently skipped by callers that check
// IsAllowedPath first.
func (d *DB) EnsureFileTracked(relPath string) (string, error) {
	f, err := d.GetFileByPath(relPath)
	if err != nil {
		return "", err
	}
	if f != nil {
		return f.FileId, nil
	}
	fullPath := filepath.Join(d.root, filepath.FromSlash(relPath))
	info, err := os.Stat(fullPath)
	if err != nil || !withinMaxFileSize(info.Size()) {
		return "", nil
	}
	b, _ := os.ReadFile(fullPath)
	folder := folderOf(relPath)
	orderNum := d.GetMaxOrderNumInFolder(folder) + 1
	return d.insertFileRecord(relPath, string(b), orderNum)
}

// NextNewN returns the smallest positive integer N not already used by a "new N" file in folderPath.
func (d *DB) NextNewN(folderPath string) int {
	var pattern string
	if folderPath == "" {
		pattern = "new %"
	} else {
		pattern = folderPath + "/new %"
	}
	rows, _ := d.sql.Query(`SELECT Path FROM files WHERE Path LIKE ?`, pattern)
	defer rows.Close()
	used := map[int]bool{}
	re := regexp.MustCompile(`/new (\d+)$|^new (\d+)$`)
	for rows.Next() {
		var p string
		rows.Scan(&p)
		m := re.FindStringSubmatch(p)
		if m != nil {
			s := m[1]
			if s == "" {
				s = m[2]
			}
			if n, err := strconv.Atoi(s); err == nil {
				used[n] = true
			}
		}
	}
	for i := 1; ; i++ {
		if !used[i] {
			return i
		}
	}
}

// NextDuplicateName returns foo (2).txt style name, incrementing until unused.
func (d *DB) NextDuplicateName(folderPath, baseName string) string {
	ext := filepath.Ext(baseName)
	stem := strings.TrimSuffix(baseName, ext)
	// strip existing " (N)" suffix from stem
	re := regexp.MustCompile(` \(\d+\)$`)
	stem = re.ReplaceAllString(stem, "")

	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		var relPath string
		if folderPath == "" {
			relPath = candidate
		} else {
			relPath = folderPath + "/" + candidate
		}
		if _, err := os.Stat(filepath.Join(d.root, filepath.FromSlash(relPath))); os.IsNotExist(err) {
			return candidate
		}
	}
}

// folderOf returns the parent folder path of a relative file path ("." becomes "").
func folderOf(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return ""
	}
	return filepath.ToSlash(dir)
}

// GetMaxOrderNumInFolder returns the max OrderNum among files directly in folderPath, or -1 if none.
func (d *DB) GetMaxOrderNumInFolder(folderPath string) int {
	var maxVal sql.NullInt64
	if folderPath == "" {
		d.sql.QueryRow(`SELECT MAX(OrderNum) FROM files WHERE Path NOT LIKE '%/%'`).Scan(&maxVal)
	} else {
		d.sql.QueryRow(
			`SELECT MAX(OrderNum) FROM files WHERE Path LIKE ? AND Path NOT LIKE ?`,
			folderPath+"/%", folderPath+"/%/%",
		).Scan(&maxVal)
	}
	if !maxVal.Valid {
		return -1
	}
	return int(maxVal.Int64)
}

// GetFilesInFolderSorted returns files directly in folderPath sorted by OrderNum ascending.
func (d *DB) GetFilesInFolderSorted(folderPath string) ([]DBFile, error) {
	var rows *sql.Rows
	var err error
	if folderPath == "" {
		rows, err = d.sql.Query(
			`SELECT FileId, Path, LastOpened, OrderNum FROM files WHERE Path NOT LIKE '%/%' ORDER BY OrderNum`,
		)
	} else {
		rows, err = d.sql.Query(
			`SELECT FileId, Path, LastOpened, OrderNum FROM files WHERE Path LIKE ? AND Path NOT LIKE ? ORDER BY OrderNum`,
			folderPath+"/%", folderPath+"/%/%",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []DBFile
	for rows.Next() {
		var f DBFile
		rows.Scan(&f.FileId, &f.Path, &f.LastOpened, &f.OrderNum)
		files = append(files, f)
	}
	return files, nil
}

// ShiftOrderNumsUp increments OrderNum by 1 for all files in folderPath where OrderNum >= fromOrderNum.
func (d *DB) ShiftOrderNumsUp(folderPath string, fromOrderNum int) error {
	var err error
	if folderPath == "" {
		_, err = d.sql.Exec(
			`UPDATE files SET OrderNum = OrderNum + 1 WHERE Path NOT LIKE '%/%' AND OrderNum >= ?`,
			fromOrderNum,
		)
	} else {
		_, err = d.sql.Exec(
			`UPDATE files SET OrderNum = OrderNum + 1 WHERE Path LIKE ? AND Path NOT LIKE ? AND OrderNum >= ?`,
			folderPath+"/%", folderPath+"/%/%", fromOrderNum,
		)
	}
	return err
}

// CompactOrderNums re-sequences OrderNums as 0,1,2… for files directly in folderPath.
func (d *DB) CompactOrderNums(folderPath string) error {
	files, err := d.GetFilesInFolderSorted(folderPath)
	if err != nil {
		return err
	}
	for i, f := range files {
		d.sql.Exec(`UPDATE files SET OrderNum = ? WHERE FileId = ?`, i, f.FileId)
	}
	return nil
}

// SetOrderNum sets OrderNum for a single file.
func (d *DB) SetOrderNum(fileId string, orderNum int) error {
	_, err := d.sql.Exec(`UPDATE files SET OrderNum = ? WHERE FileId = ?`, orderNum, fileId)
	return err
}
