package main

import (
	"log/slog"
	"net/http"
"strings"

	_ "embed"
)

//go:embed index.html
var adminHTML []byte

// Server holds all dependencies and implements http.Handler.
type Server struct {
	cfg *Config
	dbm *DBManager
	bkm *BackupManager
	mux *http.ServeMux
}

func newServer(cfg *Config, dbm *DBManager, bkm *BackupManager) *Server {
	s := &Server{cfg: cfg, dbm: dbm, bkm: bkm, mux: http.NewServeMux()}
	s.setupRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.corsMiddleware(s.mux).ServeHTTP(w, r)
}

func (s *Server) setupRoutes() {
	auth := s.authMiddleware

	// No auth
	s.mux.HandleFunc("GET /", s.landingPage)
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /docs/api", s.docsUI)
	s.mux.HandleFunc("GET /openapi.json", s.openAPISpec)

	if s.cfg.AdminEnabled {
		s.mux.HandleFunc("GET /admin/", s.adminUI)
		s.mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		})
	}

	// Databases — literal paths must be registered before wildcard paths
	s.mux.Handle("GET /api/databases", auth(http.HandlerFunc(s.listDatabases)))
	s.mux.Handle("POST /api/databases", auth(http.HandlerFunc(s.registerDatabase)))
	s.mux.Handle("POST /api/databases/upload", auth(http.HandlerFunc(s.uploadDatabase)))
	s.mux.Handle("POST /api/databases/import", auth(http.HandlerFunc(s.importDatabase)))
	s.mux.Handle("POST /api/databases/backup-all", auth(http.HandlerFunc(s.backupAll)))
	s.mux.Handle("DELETE /api/databases/{db}", auth(http.HandlerFunc(s.unregisterDatabase)))
	s.mux.Handle("GET /api/databases/{db}/info", auth(http.HandlerFunc(s.databaseInfo)))

	// Tables
	s.mux.Handle("GET /api/databases/{db}/tables", auth(http.HandlerFunc(s.listTables)))
	s.mux.Handle("GET /api/databases/{db}/tables/{table}/schema", auth(http.HandlerFunc(s.tableSchema)))
	s.mux.Handle("GET /api/databases/{db}/tables/{table}/ddl", auth(http.HandlerFunc(s.tableDDL)))

	// Rows — rowid variants must come before the generic {row_id} wildcard
	s.mux.Handle("GET /api/databases/{db}/tables/{table}/rows", auth(http.HandlerFunc(s.listRows)))
	s.mux.Handle("POST /api/databases/{db}/tables/{table}/rows", auth(http.HandlerFunc(s.createRow)))
	s.mux.Handle("PUT /api/databases/{db}/tables/{table}/rows/rowid/{rowid}", auth(http.HandlerFunc(s.updateRowByRowid)))
	s.mux.Handle("DELETE /api/databases/{db}/tables/{table}/rows/rowid/{rowid}", auth(http.HandlerFunc(s.deleteRowByRowid)))
	s.mux.Handle("GET /api/databases/{db}/tables/{table}/rows/{row_id}", auth(http.HandlerFunc(s.getRow)))
	s.mux.Handle("PUT /api/databases/{db}/tables/{table}/rows/{row_id}", auth(http.HandlerFunc(s.updateRow)))
	s.mux.Handle("DELETE /api/databases/{db}/tables/{table}/rows/{row_id}", auth(http.HandlerFunc(s.deleteRow)))

	// Query
	s.mux.Handle("POST /api/databases/{db}/query", auth(http.HandlerFunc(s.executeQuery)))

	// Backups
	s.mux.Handle("POST /api/databases/{db}/backup", auth(http.HandlerFunc(s.createBackup)))
	s.mux.Handle("GET /api/databases/{db}/backups", auth(http.HandlerFunc(s.listBackups)))
	s.mux.Handle("GET /api/databases/{db}/backups/{filename}", auth(http.HandlerFunc(s.downloadBackup)))
	s.mux.Handle("POST /api/databases/{db}/backups/{filename}/restore", auth(http.HandlerFunc(s.restoreBackup)))
	s.mux.Handle("DELETE /api/databases/{db}/backups/{filename}", auth(http.HandlerFunc(s.deleteBackup)))
}

// authMiddleware validates the X-API-Key header when API keys are configured.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if len(s.cfg.APIKeys) == 0 {
		// Dev mode: no auth required (logged once at startup)
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		for _, k := range s.cfg.APIKeys {
			if k == key {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "Invalid or missing API key")
	})
}

// corsMiddleware adds CORS headers to every response.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	origins := strings.Join(s.cfg.CORSOrigins, ",")
	allowAll := origins == "*"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			for _, o := range s.cfg.CORSOrigins {
				if o == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"dev_mode": len(s.cfg.APIKeys) == 0,
	})
}

func (s *Server) adminUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(adminHTML) //nolint:errcheck
}

func (s *Server) landingPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>EasyDB</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    background: #111111;
    color: #ededed;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  .container {
    max-width: 800px;
    width: 100%;
    padding: 6rem 2rem 4rem;
  }
  .badge {
    display: inline-block;
    padding: 4px 14px;
    border: 1px solid #2a2a2a;
    border-radius: 9999px;
    font-size: 13px;
    color: #888;
    margin-bottom: 1.5rem;
  }
  h1 {
    font-size: 3.5rem;
    font-weight: 600;
    letter-spacing: -0.03em;
    line-height: 1.1;
    margin-bottom: 1rem;
    background: linear-gradient(135deg, #ededed 60%, #3ecf8e);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }
  .tagline {
    font-size: 1.15rem;
    color: #888;
    line-height: 1.6;
    max-width: 520px;
    margin-bottom: 3rem;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 1px;
    background: #2a2a2a;
    border: 1px solid #2a2a2a;
    border-radius: 12px;
    overflow: hidden;
  }
  .card {
    background: #171717;
    padding: 1.5rem;
    transition: background 0.15s;
  }
  .card:hover { background: #1c1c1c; }
  .card h3 {
    font-size: 0.95rem;
    font-weight: 500;
    margin-bottom: 0.4rem;
  }
  .card p {
    font-size: 0.85rem;
    color: #777;
    line-height: 1.5;
  }
  .card .path {
    display: inline-block;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.8rem;
    color: #3ecf8e;
    margin-bottom: 0.6rem;
  }
  footer {
    margin-top: 4rem;
    padding: 2rem;
    text-align: center;
    font-size: 0.8rem;
    color: #444;
  }
  a { color: #3ecf8e; text-decoration: none; }
  a:hover { text-decoration: underline; }
</style>
</head>
<body>
<div class="container">
  <span class="badge">v1.0.0</span>
  <h1>EasyDB</h1>
  <p class="tagline">
    RESTful HTTP API for SQLite. Multi-database management with auto-generated
    CRUD endpoints, raw SQL, and an admin console.
  </p>
  <div class="grid">
    <a href="/admin" class="card">
      <span class="path">/admin</span>
      <h3>Admin Console</h3>
      <p>Browser-based UI for managing databases.</p>
    </a>
    <a href="/docs/api" class="card">
      <span class="path">/docs/api</span>
      <h3>API Reference</h3>
      <p>Interactive Swagger docs for all endpoints.</p>
    </a>
    <a href="/health" class="card">
      <span class="path">/health</span>
      <h3>Health Check</h3>
      <p>Server status and configuration info.</p>
    </a>
  </div>
</div>
<footer>Powered by <a href="https://github.com/huyng/easydb">EasyDB</a></footer>
</body>
</html>`)) //nolint:errcheck
	slog.Debug("landing page served")
}
