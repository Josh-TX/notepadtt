package backend

import (
	"database/sql"
	"sync"
	"time"
)

const (
	ftsDebounceWindow = 2 * time.Second
	ftsMaxWait        = 10 * time.Second
)

// ftsCacheEntry is the last-synced (rowid, Path, Content) for one FileId, reflecting
// what's currently indexed in files_fts. Needed because files_fts is an external
// content table: it stores no copy of the text itself, so removing a stale rowid's
// postings on the next sync requires remembering what was actually indexed.
type ftsCacheEntry struct {
	rowid   int64
	path    string
	content string
}

// ftsSyncState holds the debounce dirty-set/timers and the last-synced cache backing
// DB.MarkDirty. Mirrors the dirty-set + AfterFunc idiom used by Hub.BroadcastFS.
type ftsSyncState struct {
	mu            sync.Mutex
	dirty         map[string]struct{}
	cache         map[string]ftsCacheEntry
	debounceTimer *time.Timer
	maxWaitTimer  *time.Timer
}

// MarkDirty schedules fileId's row to be re-synced into files_fts. Every call site
// that mutates files (insert/content update/rename/delete) calls this; the actual
// sync happens on a debounced background flush so per-keystroke writes never pay
// FTS5 trigram tokenization cost on the hot path.
//
// Flushing uses "debounce with max wait": a 2s timer resets on every call, and a
// separate 10s timer starts on the first call of a batch and never resets, so a
// flush is never more than 10s away even under continuous activity.
func (d *DB) MarkDirty(fileId string) {
	d.fts.mu.Lock()
	defer d.fts.mu.Unlock()

	if d.fts.dirty == nil {
		d.fts.dirty = map[string]struct{}{}
	}
	d.fts.dirty[fileId] = struct{}{}

	if d.fts.debounceTimer != nil {
		d.fts.debounceTimer.Stop()
	}
	d.fts.debounceTimer = time.AfterFunc(ftsDebounceWindow, d.flushFts)

	if d.fts.maxWaitTimer == nil {
		d.fts.maxWaitTimer = time.AfterFunc(ftsMaxWait, d.flushFts)
	}
}

// flushFts re-syncs every currently-dirty FileId into files_fts: deletes whatever the
// cache says is currently indexed (if anything), then inserts the row's current
// (Path, Content) if it still exists in files, or just drops the cache entry if not.
func (d *DB) flushFts() {
	d.fts.mu.Lock()
	dirty := d.fts.dirty
	d.fts.dirty = nil
	if d.fts.debounceTimer != nil {
		d.fts.debounceTimer.Stop()
		d.fts.debounceTimer = nil
	}
	if d.fts.maxWaitTimer != nil {
		d.fts.maxWaitTimer.Stop()
		d.fts.maxWaitTimer = nil
	}
	d.fts.mu.Unlock()

	for fileId := range dirty {
		d.fts.mu.Lock()
		old, hadOld := d.fts.cache[fileId]
		d.fts.mu.Unlock()

		var rowid int64
		var path, content string
		err := d.sql.QueryRow(`SELECT Id, Path, Content FROM files WHERE FileId=?`, fileId).Scan(&rowid, &path, &content)

		if hadOld {
			// Must use the cached old values, not a fresh read of files: by flush
			// time files.Content already holds the newest keystroke's value, not
			// what's actually still reflected in the index.
			d.sql.Exec(`INSERT INTO files_fts(files_fts, rowid, Path, Content) VALUES('delete', ?, ?, ?)`,
				old.rowid, old.path, old.content)
		}

		if err == sql.ErrNoRows {
			d.fts.mu.Lock()
			delete(d.fts.cache, fileId)
			d.fts.mu.Unlock()
			continue
		}
		if err != nil {
			continue
		}

		d.sql.Exec(`INSERT INTO files_fts(rowid, Path, Content) VALUES (?, ?, ?)`, rowid, path, content)
		d.fts.mu.Lock()
		if d.fts.cache == nil {
			d.fts.cache = map[string]ftsCacheEntry{}
		}
		d.fts.cache[fileId] = ftsCacheEntry{rowid: rowid, path: path, content: content}
		d.fts.mu.Unlock()
	}
}
