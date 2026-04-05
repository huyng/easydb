# EasyDB — Project Context

## What it is
A lightweight HTTP server that exposes SQLite databases as a RESTful API. One server manages multiple databases, with auto-generated CRUD endpoints per table, a raw SQL query endpoint, automated backups, and an optional browser-based admin UI.

## Tech stack
- **Backend**: Go (stdlib `net/http`), no external HTTP framework
- **Database driver**: `modernc.org/sqlite` — pure-Go SQLite driver
- **Frontend**: Single self-contained `index.html` using React 18 (CDN), Babel (in-browser JSX), Tailwind CSS (Play CDN), and CodeMirror 5

## Project structure
```
go-easydb/
├── go.mod
├── go.sum
└── cmd/easydb/
    ├── main.go               # Entry point, CLI flags, signal handling
    ├── config.go             # Environment variable configuration
    ├── server.go             # HTTP server, route setup, middleware, //go:embed index.html
    ├── db_manager.go         # Database registry, connection pooling, quoteID()
    ├── databases.go          # /api/databases handlers
    ├── tables.go             # /api/databases/{db}/tables + row CRUD handlers
    ├── query.go              # /api/databases/{db}/query — raw SQL handler
    ├── backup_manager.go     # Backup lifecycle: create, restore, rotate, schedule
    ├── backups_handler.go    # /api/databases/{db}/backups HTTP handlers
    ├── storage.go            # LocalStorage backup backend
    ├── helpers.go            # Shared utilities: quoteID, writeJSON, scanRows, queryParam
    ├── docs.go               # OpenAPI spec generation + Swagger UI handler
    └── index.html            # Self-contained admin UI (React + Tailwind + CodeMirror)
```

## How to run
```bash
go build -o easydb ./cmd/easydb/   # build binary
./easydb                           # starts server on 127.0.0.1:8000
./easydb --db path/to/file         # start and auto-register a SQLite file
./easydb --host 0.0.0.0 --port 3000
```

## Key conventions
- Single-package Go application; all files are in `package main`
- SQLite identifiers are always double-quoted via `quoteID()`, values use `?` parameterisation
- WAL mode + foreign keys + 5s busy timeout on every connection (`openSQLite()`)
- `MaxOpenConns(1)` per database — intentional for SQLite's file-level locking; WAL enables concurrent reads
- Database names validated against `[a-zA-Z0-9_-]{1,64}`
- Admin UI is a single HTML file embedded via `//go:embed index.html`; no build step
- No raw SQL in the frontend — all queries go through dedicated API endpoints
- Comments should be concise and clear; doc comments on exported functions; inline comments only where logic isn't self-evident
- Router uses Go 1.22+ `http.ServeMux` pattern syntax (`GET /path/{param}`)

## Configuration
All via environment variables or `.env` file: `API_KEYS`, `DATA_DIR`, `ADMIN_ENABLED`, `CORS_ORIGINS`, `HOST`, `PORT`, `BACKUP_DIR`, `BACKUP_MAX_COUNT`, `BACKUP_SCHEDULE`, `EASYDB_OPEN`

## CLI
All args are flags (no positional arguments). Flags: `--db`, `--host`, `--port`, `--data-dir`

## Roadmap
- (none currently planned)

---

## Preferred Coding Style

### CLI design
- Use `--flag` arguments only, no positional arguments. Explicit is better than implicit.

### Separation of concerns
- Keep SQL in the backend. Frontend should call REST endpoints, not send raw SQL (except through the dedicated `/query` endpoint meant for user-written ad-hoc queries).
- If the frontend needs data that requires a SQL query, create a proper API endpoint for it.

### Dependencies
- Try to minimize the use of third party dependencies, unless the user specifies that is okay.
- Prefer Go standard library over third-party packages.
- If a dependency has already been added, prefer the use of existing dependencies rather than adding new ones.

### Comments
- Concise and clear. One-liners that explain "why", not "what".
- Doc comments on exported functions and types explaining purpose and notable behaviour.
- Inline comments only where the logic is non-obvious (e.g. "fetchmany +1 to detect truncation").

### Go packaging
- Single-package application (`package main`). No sub-packages unless the codebase significantly grows.
- `go.mod` + `go.sum` only. Use `go build` to produce the binary.

### Frontend
- Single self-contained HTML file. No build step. CDN-loaded libraries only.
- Tailwind utility classes applied directly in JSX. Custom CSS only for things Tailwind can't handle (pseudo-elements, complex selectors).
- Custom Tailwind theme config in an inline `<script>` block for project-specific colours.

### Error handling
- Validate at system boundaries (user input, external APIs). Trust internal code.
- Use the `httpError` helper / `handleErr()` pattern for HTTP error responses. Don't over-wrap.

### Iterative development
- Build the simplest version first, then iterate. Don't over-engineer upfront.
- Prefer editing existing files over creating new ones.
- Remove old files after verification, not before.
