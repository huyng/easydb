# EasyDB

EasyDB is a small, fast, self-contained Go binary that serves SQLite databases over a REST API and lets you explore SQLite databases using a simple browser UI. Auto-generated CRUD endpoints, scheduled backups, raw SQL, and a browser UI.

## Install

```bash
git clone https://github.com/huyng/easydb
cd easydb
go build -o easydb ./cmd/easydb/
```

## Usage

```bash
./easydb                          # starts on http://127.0.0.1:8000
./easydb --db path/to/file.db     # auto-register a database on startup
./easydb --host 0.0.0.0 --port 3000
```


## Configuration

All configuration is via environment variables. CLI flags (`--host`, `--port`, `--data-dir`, `--db`) take precedence.

| Variable | Default | Description |
|---|---|---|
| `EASYDB_API_KEYS` | _(none)_ | Comma-separated API keys. Unset = open-access mode. |
| `EASYDB_HOST` | `127.0.0.1` | Bind address. |
| `EASYDB_PORT` | `8000` | Port. |
| `EASYDB_DATA_DIR` | `data` | Directory for databases and registry. |
| `EASYDB_ADMIN_ENABLED` | `true` | Set to `false` to disable the admin UI. |
| `EASYDB_CORS_ORIGINS` | `*` | Comma-separated allowed origins. |
| `EASYDB_BACKUP_DIR` | `data/backups` | Directory for backup files. |
| `EASYDB_BACKUP_MAX_COUNT` | `5` | Max backups per database before oldest are deleted. |
| `EASYDB_BACKUP_SCHEDULE` | _(none)_ | Auto-backup interval: `30m`, `6h`, `1d`, etc. |
| `EASYDB_OPEN` | _(none)_ | SQLite file to register on startup (same as `--db`). |
| `EASYDB_S3_BUCKET` | _(none)_ | S3 bucket name. Enables S3 backup backend when set. |
| `EASYDB_S3_PREFIX` | `easydb-backups/` | Key prefix for S3 objects. |
| `EASYDB_S3_REGION` | `us-east-1` | AWS region. |
| `EASYDB_S3_ENDPOINT` | _(none)_ | Custom S3 endpoint for MinIO, Cloudflare R2, etc. |

## Authentication

Set `EASYDB_API_KEYS` to require an `X-API-Key` header on all requests:

```bash
EASYDB_API_KEYS=secret1,secret2 ./easydb
curl -H "X-API-Key: secret1" http://localhost:8000/api/databases
```

When unset, the server runs in open-access mode with no authentication.

## API

Full interactive docs at `/docs/api`. Quick reference:

**Databases**
```
GET    /api/databases
POST   /api/databases          {"name": "mydb"}
DELETE /api/databases/{db}
GET    /api/databases/{db}/info
```

**Tables & rows**
```
GET    /api/databases/{db}/tables
GET    /api/databases/{db}/tables/{table}/rows?limit=50&offset=0&order_by=id&order_dir=ASC
POST   /api/databases/{db}/tables/{table}/rows
PUT    /api/databases/{db}/tables/{table}/rows/{id}
DELETE /api/databases/{db}/tables/{table}/rows/{id}
```

**Raw SQL**
```
POST   /api/databases/{db}/query
```
Results capped at 10 000 rows.

**Backups**
```
POST   /api/databases/{db}/backup
GET    /api/databases/{db}/backups
POST   /api/databases/{db}/backups/{filename}/restore
DELETE /api/databases/{db}/backups/{filename}
```

## Security notes

- Table/column names are double-quoted and whitelisted against `sqlite_master`; values use `?` parameterisation.
- Database names validated against `[a-zA-Z0-9_-]{1,64}`.
- Import-from-URL blocks private/loopback IP ranges (SSRF protection).
- WAL mode + 5-second busy timeout on every connection.
- Set `EASYDB_CORS_ORIGINS` to specific origins in production.
