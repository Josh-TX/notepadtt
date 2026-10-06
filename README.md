# Notepadtt

A simple notepad app inspired by notepad++. Notes are auto-saved and synchronized in real time. Works will with existing files/folders in the directory. 

## Usage

```
notepadtt [-d|-directory <path>] [-p|-port <port>]
```

- `-d`, `-directory`: root directory to serve (default `.`)
- `-p`, `-port`: port to listen on (default `8080`)


## Notes

- Workspace search (Ctrl+Shift+F) shells out to `rg` (preferred) or `grep`, so one of them must be on `PATH`.
- Deleted files/folders sit in `<root>/.ntt-trash` for 5 seconds so they can be restored.
- `.git`, `node_modules` and `.ntt-trash` are hidden from the tree, watcher, and search.
