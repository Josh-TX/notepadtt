package backend

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

// Settings is the single persisted, app-wide settings record: one row, one column
// per field. TabCloseIcon is read by TabBar.vue (frontend) to decide whether to show
// a tab's close icon — see specs/settings.md. WordWrap, the 7 duration fields (TrashTTL
// + the 6 File History TTL/MinDelay values), and the search fields below are also live.
type Settings struct {
	TabCloseIcon      string `json:"tabCloseIcon"` // "visible" | "hidden" | "new"
	WordWrap          bool   `json:"wordWrap"`     // global word wrap toggle; also updatable via PUT /api/settings/wordwrap
	CtrlFSearch       bool   `json:"ctrlFSearch"`  // true = Ctrl+F opens the Search Modal; false = native browser find
	LinesPerResult    int    `json:"linesPerResult"`
	MaxResultsPerFile int    `json:"maxResultsPerFile"`
	MaxFiles          int    `json:"maxFiles"`      // cap on distinct files returned by a search, across all sources combined
	MaxFileSizeKB     int    `json:"maxFileSizeKB"` // files (on-disk size, 1 KB = 1000 bytes) over this are tracked (visible in FileTree) but their content is never read into the DB, and GET /api/files/{id} 400s instead of serving them

	TrashTTL string `json:"trashTTL"`

	ShortTermTTL      string `json:"shortTermTTL"`
	ShortTermMinDelay string `json:"shortTermMinDelay"`
	MedTermTTL        string `json:"medTermTTL"`
	MedTermMinDelay   string `json:"medTermMinDelay"`
	LongTermTTL       string `json:"longTermTTL"`
	LongTermMinDelay  string `json:"longTermMinDelay"`

	EditorFontSize     int    `json:"editorFontSize"`     // px; clamped to [10,24] on save
	SidebarWidth       int    `json:"sidebarWidth"`       // px; not editable via the Settings modal — see UpdateSidebarWidth/PUT /api/settings/sidebarwidth
	DesktopSidebarOpen bool   `json:"desktopSidebarOpen"` // not editable via the Settings modal — see UpdateDesktopSidebarOpen/PUT /api/settings/desktopsidebaropen
	MarkdownMode       int    `json:"markdownMode"`       // 0=all new files, 1=all files without extension, 2=only .md files
	ColorOverrides     string `json:"colorOverrides"`     // comma-separated key=color pairs, e.g. "keyword=#569cd6, header=#4babfd"
	Title              string `json:"title"`              // HTML <title> tag value
	OnlyTextExt        bool   `json:"onlyTextExt"`        // "File Extension to Track": true = only common text-file extensions are tracked; false = all files
}

// SettingsResponse is what GET/PUT /api/settings actually serialize: the persisted
// Settings plus the read-only TextExtensions list (from the backend's allowedExtensions
// map), letting the frontend predict IsAllowedPath's outcome — e.g. to warn before a
// rename that would untrack a file — without a round-trip. TextExtensions is kept out
// of the Settings struct itself (rather than populated by setSettingsCache) so Settings
// stays comparable with ==, which the test suite relies on.
type SettingsResponse struct {
	Settings
	TextExtensions []string `json:"textExtensions"`
}

func settingsResponse(s Settings) SettingsResponse {
	return SettingsResponse{Settings: s, TextExtensions: textExtensionsList}
}

func defaultSettings() Settings {
	return Settings{
		TabCloseIcon:      "new",
		WordWrap:          true,
		CtrlFSearch:       true,
		LinesPerResult:    4,
		MaxResultsPerFile: 2,
		MaxFiles:          30,
		MaxFileSizeKB:     1000,

		TrashTTL: "30d",

		ShortTermTTL:      "10m",
		ShortTermMinDelay: "40s",
		MedTermTTL:        "120m",
		MedTermMinDelay:   "8m",
		LongTermTTL:       "7d",
		LongTermMinDelay:  "18h",

		EditorFontSize:     14,
		SidebarWidth:       400,
		DesktopSidebarOpen: false,
		MarkdownMode:       0,
		ColorOverrides:     "keyword=#569cd6, header=#54b0ff",
		Title:              "notepadtt",
		OnlyTextExt:        true,
	}
}

func createSettingsSchema(sqldb *sql.DB) error {
	_, err := sqldb.Exec(`CREATE TABLE IF NOT EXISTS Settings (
		Id INTEGER PRIMARY KEY AUTOINCREMENT,
		TabCloseIcon TEXT,
		WordWrap INTEGER,
		CtrlFSearch INTEGER,
		LinesPerResult INTEGER,
		MaxResultsPerFile INTEGER,
		MaxFiles INTEGER,
		TrashTTL TEXT,
		ShortTermTTL TEXT,
		ShortTermMinDelay TEXT,
		MedTermTTL TEXT,
		MedTermMinDelay TEXT,
		LongTermTTL TEXT,
		LongTermMinDelay TEXT,
		EditorFontSize INTEGER,
		SidebarWidth INTEGER,
		DesktopSidebarOpen INTEGER,
		MarkdownMode INTEGER,
		ColorOverrides TEXT,
		Title TEXT,
		OnlyTextExt INTEGER
	)`)
	if err != nil {
		return err
	}
	// Migration for prod DBs created before OnlyTextExt existed: CREATE TABLE IF NOT
	// EXISTS above is a no-op against an already-existing Settings table, so the column
	// needs to be added explicitly if missing.
	if err := addColumnIfMissing(sqldb, "Settings", "OnlyTextExt", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	// Migration for prod DBs created before MaxFileSizeKB existed.
	if err := addColumnIfMissing(sqldb, "Settings", "MaxFileSizeKB", fmt.Sprintf("INTEGER NOT NULL DEFAULT %d", defaultSettings().MaxFileSizeKB)); err != nil {
		return err
	}
	var count int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM Settings`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return insertSettingsRow(sqldb, defaultSettings())
}

// addColumnIfMissing runs `ALTER TABLE ... ADD COLUMN` only if the column doesn't
// already exist, making schema evolution idempotent without a migration framework.
func addColumnIfMissing(sqldb *sql.DB, table, column, decl string) error {
	rows, err := sqldb.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = sqldb.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

func insertSettingsRow(sqldb *sql.DB, s Settings) error {
	_, err := sqldb.Exec(`INSERT INTO Settings (
		TabCloseIcon, WordWrap, CtrlFSearch,
		LinesPerResult, MaxResultsPerFile, MaxFiles, MaxFileSizeKB, TrashTTL, ShortTermTTL, ShortTermMinDelay,
		MedTermTTL, MedTermMinDelay, LongTermTTL, LongTermMinDelay,
		EditorFontSize, SidebarWidth, DesktopSidebarOpen, MarkdownMode, ColorOverrides, Title, OnlyTextExt
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.TabCloseIcon, s.WordWrap, s.CtrlFSearch,
		s.LinesPerResult, s.MaxResultsPerFile, s.MaxFiles, s.MaxFileSizeKB, s.TrashTTL, s.ShortTermTTL, s.ShortTermMinDelay,
		s.MedTermTTL, s.MedTermMinDelay, s.LongTermTTL, s.LongTermMinDelay,
		s.EditorFontSize, s.SidebarWidth, s.DesktopSidebarOpen, s.MarkdownMode, s.ColorOverrides, s.Title, s.OnlyTextExt)
	return err
}

// GetSettings reads the single Settings row. The table always has exactly one row
// (seeded by createSettingsSchema), so no Id/WHERE clause is needed.
func (d *DB) GetSettings() (Settings, error) {
	var s Settings
	err := d.sql.QueryRow(`SELECT
		TabCloseIcon, WordWrap, CtrlFSearch,
		LinesPerResult, MaxResultsPerFile, MaxFiles, MaxFileSizeKB, TrashTTL, ShortTermTTL, ShortTermMinDelay,
		MedTermTTL, MedTermMinDelay, LongTermTTL, LongTermMinDelay,
		EditorFontSize, SidebarWidth, DesktopSidebarOpen, MarkdownMode, ColorOverrides, Title, OnlyTextExt
		FROM Settings LIMIT 1`).
		Scan(&s.TabCloseIcon, &s.WordWrap, &s.CtrlFSearch,
			&s.LinesPerResult, &s.MaxResultsPerFile, &s.MaxFiles, &s.MaxFileSizeKB, &s.TrashTTL, &s.ShortTermTTL, &s.ShortTermMinDelay,
			&s.MedTermTTL, &s.MedTermMinDelay, &s.LongTermTTL, &s.LongTermMinDelay,
			&s.EditorFontSize, &s.SidebarWidth, &s.DesktopSidebarOpen, &s.MarkdownMode, &s.ColorOverrides, &s.Title, &s.OnlyTextExt)
	return s, err
}

// SaveSettings overwrites the single Settings row in full — there's no per-field PATCH
// for the rest of the form, the Settings modal always saves every field at once. WordWrap,
// SidebarWidth, and DesktopSidebarOpen also each have their own narrow endpoint
// (UpdateWordWrap/PUT /api/settings/wordwrap, UpdateSidebarWidth/PUT /api/settings/sidebarwidth,
// UpdateDesktopSidebarOpen/PUT /api/settings/desktopsidebaropen) for callers that shouldn't go
// through the full form, but a full-form save still writes them too — the frontend always
// folds the live store value into the form for all three fields first.
func (d *DB) SaveSettings(s Settings) error {
	_, err := d.sql.Exec(`UPDATE Settings SET
		TabCloseIcon=?, WordWrap=?, CtrlFSearch=?,
		LinesPerResult=?, MaxResultsPerFile=?, MaxFiles=?, MaxFileSizeKB=?, TrashTTL=?, ShortTermTTL=?, ShortTermMinDelay=?,
		MedTermTTL=?, MedTermMinDelay=?, LongTermTTL=?, LongTermMinDelay=?,
		EditorFontSize=?, SidebarWidth=?, DesktopSidebarOpen=?, MarkdownMode=?, ColorOverrides=?, Title=?, OnlyTextExt=?`,
		s.TabCloseIcon, s.WordWrap, s.CtrlFSearch,
		s.LinesPerResult, s.MaxResultsPerFile, s.MaxFiles, s.MaxFileSizeKB, s.TrashTTL, s.ShortTermTTL, s.ShortTermMinDelay,
		s.MedTermTTL, s.MedTermMinDelay, s.LongTermTTL, s.LongTermMinDelay,
		s.EditorFontSize, s.SidebarWidth, s.DesktopSidebarOpen, s.MarkdownMode, s.ColorOverrides, s.Title, s.OnlyTextExt)
	return err
}

// UpdateSidebarWidth persists just the SidebarWidth column, independent of the rest of
// the Settings row — backs the Sidebar's drag-to-resize handle (PUT /api/settings/sidebarwidth),
// which is never exposed in the Settings modal. width is clamped to [100,600] by the caller.
func (d *DB) UpdateSidebarWidth(width int) error {
	_, err := d.sql.Exec(`UPDATE Settings SET SidebarWidth=?`, width)
	return err
}

// UpdateDesktopSidebarOpen persists just the DesktopSidebarOpen column, independent of the
// rest of the Settings row — backs the desktop (push-layout) sidebar toggle/collapse
// (PUT /api/settings/desktopsidebaropen), which is never exposed in the Settings modal.
func (d *DB) UpdateDesktopSidebarOpen(open bool) error {
	_, err := d.sql.Exec(`UPDATE Settings SET DesktopSidebarOpen=?`, open)
	return err
}

// UpdateWordWrap persists just the WordWrap column, independent of the rest of the
// Settings row — backs the Footer's wrap toggle (PUT /api/settings/wordwrap), which
// shouldn't be coupled to the Settings modal's full-form save/validation.
func (d *DB) UpdateWordWrap(wrap bool) error {
	_, err := d.sql.Exec(`UPDATE Settings SET WordWrap=?`, wrap)
	return err
}

// clamp silently constrains v to [lo,hi], used for EditorFontSize and SidebarWidth —
// unlike the rest of validateSettings, out-of-range values here are clamped rather
// than rejected.
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validateSettings rejects the whole save atomically if anything is invalid: a
// duration string in the wrong format, TTL or MinDelay tiers that aren't strictly
// increasing (the FileVersioning process's cascading-tier logic depends on this), or a
// non-positive search-result count. Zero-valued durations (e.g. "0s") are allowed.
// A tier's MinDelay vs. its own TTL is deliberately not cross-checked.
func validateSettings(s Settings) error {
	durations := []struct {
		name string
		raw  string
	}{
		{"trashTTL", s.TrashTTL},
		{"shortTermTTL", s.ShortTermTTL}, {"shortTermMinDelay", s.ShortTermMinDelay},
		{"medTermTTL", s.MedTermTTL}, {"medTermMinDelay", s.MedTermMinDelay},
		{"longTermTTL", s.LongTermTTL}, {"longTermMinDelay", s.LongTermMinDelay},
	}
	parsed := map[string]int64{}
	for _, d := range durations {
		dur, err := parseDuration(d.raw)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", d.name, d.raw, err)
		}
		parsed[d.name] = int64(dur)
	}

	if !(parsed["shortTermTTL"] < parsed["medTermTTL"] &&
		parsed["medTermTTL"] < parsed["longTermTTL"]) {
		return fmt.Errorf("TTL tiers must be strictly increasing: shortTermTTL < medTermTTL < longTermTTL")
	}
	if !(parsed["shortTermMinDelay"] < parsed["medTermMinDelay"] &&
		parsed["medTermMinDelay"] < parsed["longTermMinDelay"]) {
		return fmt.Errorf("MinDelay tiers must be strictly increasing: shortTermMinDelay < medTermMinDelay < longTermMinDelay")
	}

	if s.LinesPerResult < 1 {
		return fmt.Errorf("linesPerResult must be >= 1")
	}
	if s.MaxResultsPerFile < 1 {
		return fmt.Errorf("maxResultsPerFile must be >= 1")
	}
	if s.MaxFiles < 1 {
		return fmt.Errorf("maxFiles must be >= 1")
	}
	if s.MaxFileSizeKB < 1 {
		return fmt.Errorf("maxFileSizeKB must be >= 1")
	}
	return nil
}

var settingsMu sync.RWMutex
var currentSettings Settings

// GetSettingsCache returns the in-memory settings, serving GET /api/settings without a
// DB round-trip on every request.
func GetSettingsCache() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return currentSettings
}

// setSettingsCache updates the in-memory settings (read by GET /api/settings) and
// re-derives the parsed retention durations the FileVersioning process reads every
// run (see applyRetentionSettings in retention.go). Called once at startup and again
// after every successful PUT /api/settings, so a saved change is picked up by the next
// FileVersioning run without a server restart.
func setSettingsCache(s Settings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if err := applyRetentionSettings(s); err != nil {
		return err
	}
	currentSettings = s
	return nil
}

// setSidebarWidthCache updates just the cached SidebarWidth field, mirroring
// setWordWrapCache's independence from the rest of the Settings row.
func setSidebarWidthCache(width int) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	currentSettings.SidebarWidth = width
}

// setWordWrapCache updates just the cached WordWrap field, mirroring UpdateWordWrap's
// independence from the rest of the Settings row.
func setWordWrapCache(wrap bool) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	currentSettings.WordWrap = wrap
}

// setDesktopSidebarOpenCache updates just the cached DesktopSidebarOpen field, mirroring
// UpdateDesktopSidebarOpen's independence from the rest of the Settings row.
func setDesktopSidebarOpenCache(open bool) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	currentSettings.DesktopSidebarOpen = open
}
