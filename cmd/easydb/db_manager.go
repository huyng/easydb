package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	_ "modernc.org/sqlite"
)

var dbNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,64}$`)

// DBInfo describes a registered database.
type DBInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
}

// DBManager manages the registry of databases and their open connections.
type DBManager struct {
	dataDir         string
	exploratoryMode bool
	mu              sync.RWMutex
	registry        map[string]string // name → absolute path
	conns           map[string]*sql.DB
}

func newDBManager(dataDir string, exploratoryMode bool) (*DBManager, error) {
	m := &DBManager{
		dataDir:         dataDir,
		exploratoryMode: exploratoryMode,
		registry:        make(map[string]string),
		conns:           make(map[string]*sql.DB),
	}
	return m, m.loadRegistry()
}

// EnsureDataDir creates the data directory if it does not already exist.
// Returns true if the directory was newly created.
func (m *DBManager) EnsureDataDir() (bool, error) {
	if _, err := os.Stat(m.dataDir); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		return false, err
	}
	if err := m.saveRegistry(); err != nil {
		return true, err
	}
	return true, nil
}

// register adds a database to the registry. path defaults to dataDir/name.db.
func (m *DBManager) register(name, path string) error {
	if !dbNameRe.MatchString(name) {
		return &httpError{http.StatusBadRequest, "invalid database name: must match [a-zA-Z0-9_-]{1,64}"}
	}
	if path == "" {
		path = filepath.Join(m.dataDir, name+".db")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.registry[name]; ok {
		return &httpError{http.StatusConflict, "database already registered: " + name}
	}
	m.registry[name] = abs
	return m.saveRegistry()
}

// unregister removes a database from the registry, optionally deleting its file.
func (m *DBManager) unregister(name string, deleteFile bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path, ok := m.registry[name]
	if !ok {
		return &httpError{http.StatusNotFound, "database not found: " + name}
	}
	if db, ok := m.conns[name]; ok {
		db.Close()
		delete(m.conns, name)
	}
	delete(m.registry, name)
	if err := m.saveRegistry(); err != nil {
		return err
	}
	if deleteFile {
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
	}
	return nil
}

// list returns metadata for all registered databases.
func (m *DBManager) list() []DBInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]DBInfo, 0, len(m.registry))
	for name, path := range m.registry {
		info := DBInfo{Name: name, Path: path}
		if fi, err := os.Stat(path); err == nil {
			info.Exists = true
			info.SizeBytes = fi.Size()
		}
		out = append(out, info)
	}
	return out
}

// getPath returns the absolute path for a registered database or 404.
func (m *DBManager) getPath(name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path, ok := m.registry[name]
	if !ok {
		return "", &httpError{http.StatusNotFound, "database not found: " + name}
	}
	return path, nil
}

// openDB returns (or creates) a *sql.DB connection for the named database.
func (m *DBManager) openDB(name string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.conns[name]; ok {
		return db, nil
	}
	path, ok := m.registry[name]
	if !ok {
		return nil, &httpError{http.StatusNotFound, "database not found: " + name}
	}
	db, err := openSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	m.conns[name] = db
	return db, nil
}

// closeDB closes and removes the cached connection for name (used before restore).
func (m *DBManager) closeDB(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.conns[name]; ok {
		db.Close()
		delete(m.conns, name)
	}
}

// registryPath returns the path to the registry JSON file.
func (m *DBManager) registryPath() string {
	return filepath.Join(m.dataDir, "registry.json")
}

func (m *DBManager) loadRegistry() error {
	data, err := os.ReadFile(m.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read registry: %w", err)
	}
	return json.Unmarshal(data, &m.registry)
}

func (m *DBManager) saveRegistry() error {
	// In exploratory mode, skip writing the registry if the data dir doesn't
	// exist yet — the initial DB is tracked in memory only until EnsureDataDir
	// is called.
	if m.exploratoryMode {
		if _, err := os.Stat(m.dataDir); os.IsNotExist(err) {
			return nil
		}
	}
	data, err := json.MarshalIndent(m.registry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.registryPath(), data, 0644)
}

// openSQLite opens a SQLite database with WAL mode, foreign keys, and busy timeout.
func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Single connection: WAL allows concurrent reads; per-connection PRAGMAs stay set.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return db, nil
}

// httpError is an error that carries an HTTP status code.
type httpError struct {
	Status  int
	Message string
}

func (e *httpError) Error() string { return e.Message }

// handleErr writes an appropriate HTTP error for err. Returns true if err != nil.
func handleErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.Status, he.Message)
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
	return true
}

// ColumnInfo mirrors SQLite's PRAGMA table_info row.
type ColumnInfo struct {
	CID       int     `json:"cid"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	NotNull   int     `json:"notnull"`
	DfltValue *string `json:"dflt_value"`
	PK        int     `json:"pk"`
}

// tableColumns fetches column metadata for a table via PRAGMA table_info.
func tableColumns(db *sql.DB, table string) ([]ColumnInfo, error) {
	rows, err := db.Query("PRAGMA table_info(" + quoteID(table) + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		var dflt sql.NullString
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &c.NotNull, &dflt, &c.PK); err != nil {
			return nil, err
		}
		if dflt.Valid {
			c.DfltValue = &dflt.String
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// pkColumn returns the single primary key column for table, or "" if none or compound.
func pkColumn(cols []ColumnInfo) string {
	var pk string
	count := 0
	for _, c := range cols {
		if c.PK > 0 {
			count++
			pk = c.Name
		}
	}
	if count == 1 {
		return pk
	}
	return ""
}

// tableExists returns true if the named table exists in the database.
func tableExists(db *sql.DB, table string) (bool, error) {
	var n int
	err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
	return n > 0, err
}
