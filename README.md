# Notepadtt

A simple notepad app inspired by notepad++. Notes are auto-saved and synchronized in real time.

## Usage

```
notepadtt [-d|-directory <path>] [-p|-port <port>]
```

- `-d`, `-directory`: root directory to serve (default `.`)
- `-p`, `-port`: port to listen on (default `8080`)


## Notes

- Files are read from and written to disk directly; the server keeps no database. Open tabs and the active tab live in server memory only and reset on restart.
- Workspace search (Ctrl+Shift+F) shells out to `rg` (preferred) or `grep`, so one of them must be on `PATH`.
- Deleted files sit in `<root>/.ntt-trash` for 5 seconds so they can be undone; the folder is wiped on startup.
- `.git`, `node_modules` and `.ntt-trash` are hidden from the tree, watcher, and search.
