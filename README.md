# EasyDB

A lightweight HTTP server that exposes SQLite databases as a RESTful API. One server manages multiple databases, with auto-generated CRUD endpoints per table, a raw SQL query endpoint, automated backups, and an optional browser-based admin UI.

## Features

- **Multi-database** — register and manage multiple SQLite files from one server
- **Auto-generated CRUD** — row-level GET, POST, PUT, DELETE for every table
- **Raw SQL endpoint** — execute arbitrary queries with parameterised inputs
- **Backups** — on-demand and scheduled backups with rotation and one-click restore
- **Admin UI** — browser interface for browsing tables, running SQL, and managing backups
- **API key auth** — `X-API-Key` header, multiple keys supported
- **Safe by default** — all identifiers double-quoted, values parameterised, WAL mode enabled
- **Single binary** — no runtime dependencies; embeds the admin UI

## Quick start

```bash
# 1. Build
go build -o easydb

# 2. Run
./easydb
# or with a pre-registered database:
./easydb --db path/to/file.db
# or on a custom address:
./easydb --host 0.0.0.0 --port 3000
```

Server starts on `http://127.0.0.1:8000`. Admin UI at `http://localhost:8000/admin/`.

Interactive API docs at `http://localhost:8000/docs/api`.

## Configuration

All configuration is via environment variables (or a `.env` file). CLI flags take precedence over environment variables.

| Variable | Default | Description |
|---|---|---|
| `API_KEYS` | _(none)_ | Comma-separated API keys. If unset, server runs in open-access dev mode. |
| `DATA_DIR` | `data` | Directory where databases and the registry are stored. |
| `ADMIN_ENABLED` | `true` | Set to `false` to disable the admin UI. |
| `CORS_ORIGINS` | `*` | Comma-separated allowed origins. Use specific origins in production. |
| `HOST` | `127.0.0.1` | Bind address. |
| `PORT` | `8000` | Port. |
| `BACKUP_DIR` | `data/backups` | Directory for backup files. |
| `BACKUP_MAX_COUNT` | `5` | Maximum backups per database before oldest are auto-deleted. |
| `BACKUP_SCHEDULE` | _(none)_ | Automatic backup interval, e.g. `30m`, `6h`, `1d`. Disabled if unset. |
| `EASYDB_OPEN` | _(none)_ | SQLite file to auto-register on startup (equivalent to `--db`). |

### CLI flags

```
--db string        SQLite file to auto-register on startup
--host string      Bind address
--port string      Port number
--data-dir string  Data directory
```

## Authentication

All API endpoints require an `X-API-Key` header:

```bash
curl -H "X-API-Key: your-secret-key" http://localhost:8000/api/databases
```

Multiple keys are supported — set `API_KEYS=key1,key2,key3`. Any valid key grants full access.

When `API_KEYS` is not set, the server runs in dev mode with no authentication required.

## API reference

### Databases

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/databases` | List all registered databases |
| `POST` | `/api/databases` | Register a database by name or path |
| `POST` | `/api/databases/upload` | Upload a `.db` file |
| `POST` | `/api/databases/import` | Import a database from a URL |
| `DELETE` | `/api/databases/{db}` | Unregister a database |
| `GET` | `/api/databases/{db}/info` | Database metadata (version, journal mode, etc.) |

**Register a database:**
```bash
# Creates data/{name}.db if it doesn't exist
curl -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"name": "mydb"}' \
  http://localhost:8000/api/databases

# Point at an existing file
curl -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"name": "mydb", "path": "/data/production.db"}' \
  http://localhost:8000/api/databases
```

**Unregister** (add `?delete_file=true` to also delete the file):
```bash
curl -X DELETE -H "X-API-Key: $KEY" http://localhost:8000/api/databases/mydb
```

### Tables

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/databases/{db}/tables` | List tables with columns and row counts |
| `GET` | `/api/databases/{db}/tables/{table}/schema` | Column schema for one table |
| `GET` | `/api/databases/{db}/tables/{table}/ddl` | CREATE TABLE statement |

### Rows

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/databases/{db}/tables/{table}/rows` | List rows (paginated) |
| `POST` | `/api/databases/{db}/tables/{table}/rows` | Insert a row |
| `GET` | `/api/databases/{db}/tables/{table}/rows/{id}` | Get a row by primary key |
| `PUT` | `/api/databases/{db}/tables/{table}/rows/{id}` | Update a row |
| `DELETE` | `/api/databases/{db}/tables/{table}/rows/{id}` | Delete a row |

**List rows** — query parameters:

| Parameter | Default | Description |
|---|---|---|
| `limit` | `50` | Rows per page (max 1000) |
| `offset` | `0` | Row offset |
| `order_by` | _(none)_ | Column name to sort by |
| `order_dir` | `ASC` | `ASC` or `DESC` |

```bash
# First page
curl -H "X-API-Key: $KEY" \
  "http://localhost:8000/api/databases/mydb/tables/users/rows"

# Paginated and sorted
curl -H "X-API-Key: $KEY" \
  "http://localhost:8000/api/databases/mydb/tables/users/rows?limit=25&offset=50&order_by=created_at&order_dir=DESC"
```

**Insert:**
```bash
curl -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com", "age": 30}' \
  http://localhost:8000/api/databases/mydb/tables/users/rows
```

**Update** (partial — only supplied fields are changed):
```bash
curl -X PUT -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"age": 31}' \
  http://localhost:8000/api/databases/mydb/tables/users/rows/1
```

**Delete:**
```bash
curl -X DELETE -H "X-API-Key: $KEY" \
  http://localhost:8000/api/databases/mydb/tables/users/rows/1
```

> Row endpoints require a table with a single-column primary key. Tables without one can be accessed via `/rows/rowid/{rowid}`.

### Raw SQL

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/databases/{db}/query` | Execute a SQL statement |

```bash
# SELECT — returns columns + rows
curl -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM users WHERE age > ?", "params": [18]}' \
  http://localhost:8000/api/databases/mydb/query

# DDL / DML — returns rowcount + lastrowid
curl -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"sql": "CREATE TABLE logs (id INTEGER PRIMARY KEY, msg TEXT, ts INTEGER)"}' \
  http://localhost:8000/api/databases/mydb/query
```

SELECT results are capped at 10 000 rows. When truncated, the response includes `"truncated": true`.

### Backups

EasyDB supports on-demand and scheduled backups. Backups use SQLite's `VACUUM INTO` to produce a consistent snapshot without locking the database or interrupting concurrent reads and writes.

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/databases/{db}/backup` | Create a backup of a single database |
| `GET` | `/api/databases/{db}/backups` | List backups (newest first) |
| `GET` | `/api/databases/{db}/backups/{filename}` | Download a backup file |
| `POST` | `/api/databases/{db}/backups/{filename}/restore` | Restore from a backup |
| `DELETE` | `/api/databases/{db}/backups/{filename}` | Delete a backup |
| `POST` | `/api/databases/backup-all` | Back up all databases |

```bash
# Create
curl -X POST -H "X-API-Key: $KEY" http://localhost:8000/api/databases/mydb/backup

# List
curl -H "X-API-Key: $KEY" http://localhost:8000/api/databases/mydb/backups

# Download
curl -O -H "X-API-Key: $KEY" \
  http://localhost:8000/api/databases/mydb/backups/mydb_2026-04-03T12-00-00.db

# Restore
curl -X POST -H "X-API-Key: $KEY" \
  http://localhost:8000/api/databases/mydb/backups/mydb_2026-04-03T12-00-00.db/restore
```

#### Scheduled backups

Set `BACKUP_SCHEDULE` to automatically back up all databases at a regular interval:

```bash
BACKUP_SCHEDULE=6h   # every 6 hours
BACKUP_SCHEDULE=30m  # every 30 minutes
BACKUP_SCHEDULE=1d   # daily
```

#### Rotation

When the number of backups for a database exceeds `BACKUP_MAX_COUNT`, the oldest are automatically deleted. This applies to both on-demand and scheduled backups.

## Admin UI

Open `http://localhost:8000/admin/` in a browser. Enter your server URL and API key to connect.

- **Sidebar** — browse registered databases and their tables
- **Browse tab** — paginated, sortable table view; register new databases with the `+` button
- **SQL Query tab** — run arbitrary SQL against any database with syntax highlighting
- **Info tab** — database metadata, table listing, and full schema viewer
- **Backups tab** — view, create, download, restore, and delete backups

The API key is stored in `sessionStorage` only and cleared when the tab is closed.

Disable the UI in production by setting `ADMIN_ENABLED=false`.

## File layout

```
go-easydb/
├── go.mod               # Module definition (single dependency: modernc.org/sqlite)
├── main.go              # Entry point, CLI flags, signal handling
├── config.go            # Environment variable configuration
├── server.go            # HTTP server, routing, middleware, embedded index.html
├── db_manager.go        # Database registry, connection pooling
├── databases.go         # Database registration handlers
├── tables.go            # Table schema + row CRUD handlers
├── query.go             # Raw SQL execution handler
├── backup_manager.go    # Backup create, restore, rotate, schedule
├── backups_handler.go   # Backup HTTP handlers
├── storage.go           # LocalStorage backup backend
├── helpers.go           # Shared utilities
├── docs.go              # OpenAPI spec + Swagger UI
└── index.html           # Self-contained admin UI (React + Tailwind + CodeMirror)
```

## Security notes

- **Identifier injection** — table and column names are always double-quoted via `quoteID()` and whitelisted against `sqlite_master` before use. Values always go through `?` parameterisation.
- **Path traversal** — database names are validated against `[a-zA-Z0-9_-]{1,64}`. Backup filenames are validated against a strict timestamp pattern.
- **SSRF** — the import-from-URL endpoint validates the scheme and blocks private/loopback IP ranges.
- **Concurrency** — WAL journal mode and a 5-second busy timeout are set on every connection.
- **Result size** — raw SQL queries are capped at 10 000 rows to bound memory usage.
- **CORS** — set `CORS_ORIGINS` to your specific frontend origin(s) in production rather than `*`.
