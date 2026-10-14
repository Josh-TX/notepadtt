# Notepadtt

A simple notepad app inspired by notepad++. Notes are auto-saved and synchronized in real time. Works will with existing files/folders in the directory. 

## Usage

```
notepadtt [-d|--directory <path>] [-p|--port <port>]
```

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-d`, `--directory` | `NOTEPADTT_DIRECTORY` | `.` | root directory to serve |
| `-p`, `--port` | `NOTEPADTT_PORT` | `8080` | port to listen on |

## Building

```
make build      # builds the frontend, then the Go binary (./notepadtt) with the frontend embedded
```

The frontend is only embedded when built with `-tags embed_frontend` (`make backend` does this). A plain `go build` in `backend/` works without Node, but serves no UI. For development, run `go run .` in `backend/` and `npm run dev` in `frontend/`.

## Notes

- Workspace search (Ctrl+Shift+F) shells out to `rg` (preferred) or `grep`, so one of them must be on `PATH`.
- Deleted files/folders sit in `<root>/.ntt-trash` for 7 seconds so they can be restored.
- `.git`, `node_modules` and `.ntt-trash` are hidden from the tree, watcher, and search.
