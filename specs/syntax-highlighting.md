# Syntax Highlighting

## Description

I want CodeMirror 5 syntax highlighting for a curated set of languages, detected automatically from the filename. I also want a new `MarkdownMode` setting that controls when markdown highlighting applies to files that don't have an explicit recognized extension.

### Language support

Import only the following CM5 mode files (no others):

- `codemirror/mode/javascript/javascript` — covers `.js`, `.ts`/`.tsx`/`.jsx` (TypeScript/JSX variants), and `.json`
- `codemirror/mode/python/python` — covers `.py`
- `codemirror/mode/markdown/markdown` — covers `.md`
- `codemirror/mode/yaml/yaml` — covers `.yaml`, `.yml`
- `codemirror/mode/htmlmixed/htmlmixed` — covers `.html`, `.htm`; depends on the CSS and JS modes already loaded above, plus `codemirror/mode/xml/xml` which must also be imported
- `codemirror/mode/css/css` — covers `.css`, `.scss`, `.less`
- `codemirror/mode/shell/shell` — covers `.sh`, `.bash`, `.zsh`, `.fish`
- `codemirror/mode/sql/sql` — covers `.sql`
- `codemirror/mode/meta` — provides `CodeMirror.findModeByFileName()` for the lookup

No addons beyond what's already present. No `continuelist`. No keybindings changes.

### Language detection

When the active file changes, derive its mode from the filename using this priority order:

1. Run `CodeMirror.findModeByFileName(filename)` from `mode/meta`. If it returns a mode that is in our supported set above, use it.
2. If the result is the `markdown` mode specifically (i.e., `.md` extension), always use markdown regardless of the `MarkdownMode` setting.
3. If no recognized mode is found (unrecognized or missing extension), apply `MarkdownMode` logic (see below).

Apply the mode via `cm.setOption('mode', modeSpec)` where `modeSpec` is the MIME string from the meta result (e.g. `"text/javascript"`, `"text/x-python"`) or `"text/x-markdown"` for markdown, or `null` for plain text.

For TypeScript files (`.ts`, `.tsx`), use MIME `"text/typescript"` so CM5's JS mode enables TS parsing. For `.json`, use `"application/json"`.

### MarkdownMode logic (for files with no recognized extension)

When no recognized extension is found, the `MarkdownMode` setting (int, 0–2) determines the fallback:

- `0` — "all new files" (default): if the filename (without path) starts with `new ` (case-insensitive), use markdown; otherwise plain text.
- `1` — "all files without an extension": if the filename contains no dot at all, use markdown; otherwise plain text.
- `2` — "only .md files": always plain text (`.md` is already handled in step 2 above).

"No dot" means the bare filename (after the last `/`) has no `.` character anywhere — so `notes` matches but `notes.xyz` does not.

### Settings — new MarkdownMode field

Add a `MarkdownMode` int column to the existing `Settings` table with a default of `0`. This follows the same wide-table, single-row pattern established by `settings.md`. No backend behavior reads this value — it is fetched by the frontend as part of the existing `GET /api/settings` response and included in `PUT /api/settings` saves.

In `SettingsModal.vue`, add a new section (place it after the existing UI-preference fields and before the retention/TTL fields — exact position is flexible, use good judgment). The section label is **"markdown syntax highlighting"** and contains a `<select>` with these three options in order:

1. `all new files` (value `0`)
2. `all files without an extension` (value `1`)
3. `only .md files` (value `2`)

Wire it into the existing single-Save form flow like every other settings field.

### Footer language label

On viewports ≥768px wide, add a language label to the Footer's right side (alongside the existing "length: X lines: Y" stats). Show the friendly name of the active mode — e.g. "JavaScript", "TypeScript", "Python", "Markdown", "SQL", "CSS", "YAML", "Shell", "HTML" — or **"text"** when plain text. On viewports <768px, omit the label entirely (do not render it). Use a `matchMedia` or CSS approach consistent with how the rest of the app handles the 768px breakpoint. The label is display-only — clicking it does nothing.

## Out of Scope

- Markdown list continuation (`continuelist` addon / `newlineAndIndentContinueMarkdownList`)
- Manual language override (clicking the footer label to pick a different mode)
- Shebang-based detection for extension-less files
- Any language not in the supported set above
