package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BackupManager handles creating, listing, restoring, and deleting backups.
type BackupManager struct {
	cfg *Config
	dbm *DBManager
	st  StorageBackend
}

func newBackupManager(cfg *Config, dbm *DBManager) (*BackupManager, error) {
	var (
		st  StorageBackend
		err error
	)
	if cfg.BackupS3Bucket != "" {
		st, err = newS3Storage(cfg)
	} else {
		st, err = newLocalStorage(cfg.BackupDir)
	}
	if err != nil {
		return nil, err
	}
	return &BackupManager{cfg: cfg, dbm: dbm, st: st}, nil
}

// CreateBackup creates a backup of the named database using VACUUM INTO.
func (b *BackupManager) CreateBackup(dbName string) (*BackupMeta, error) {
	path, err := b.dbm.getPath(dbName)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, &httpError{http.StatusNotFound, "database file not found: " + path}
	}

	filename := backupFilename(dbName)

	// VACUUM INTO requires a local path, so always write to a temp file first,
	// then hand it to the storage backend (which may upload it to S3).
	tmp, err := os.CreateTemp("", "easydb-backup-*.db")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	db, err := b.dbm.openDB(dbName)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("VACUUM INTO ?", tmpPath); err != nil {
		return nil, fmt.Errorf("vacuum into: %w", err)
	}

	size, err := b.st.Put(filename, tmpPath)
	if err != nil {
		return nil, fmt.Errorf("store backup: %w", err)
	}

	b.rotateBackups(dbName)
	return &BackupMeta{
		Filename:  filename,
		SizeBytes: size,
		CreatedAt: createdAtFromFilename(filename),
	}, nil
}

// ListBackups returns all backups for a database, newest first.
func (b *BackupManager) ListBackups(dbName string) ([]BackupMeta, error) {
	return b.st.List(dbName + "_")
}

// StreamBackup writes a backup file to w. Returns 400 for invalid filenames
// and 404 if the backup does not exist.
func (b *BackupManager) StreamBackup(dbName, filename string, w io.Writer) error {
	if !backupFilenameRe.MatchString(filename) {
		return &httpError{http.StatusBadRequest, "invalid backup filename"}
	}
	ok, err := b.st.Exists(filename)
	if err != nil {
		return err
	}
	if !ok {
		return &httpError{http.StatusNotFound, "backup not found: " + filename}
	}
	return b.st.WriteTo(filename, w)
}

// RestoreBackup replaces the database file with the given backup.
func (b *BackupManager) RestoreBackup(dbName, filename string) error {
	if !backupFilenameRe.MatchString(filename) {
		return &httpError{http.StatusBadRequest, "invalid backup filename"}
	}
	if dbNameFromFilename(filename) != dbName {
		return &httpError{http.StatusBadRequest, "backup does not belong to database " + dbName}
	}

	ok, err := b.st.Exists(filename)
	if err != nil {
		return err
	}
	if !ok {
		return &httpError{http.StatusNotFound, "backup not found: " + filename}
	}

	dbPath, err := b.dbm.getPath(dbName)
	if err != nil {
		return err
	}

	// Close the connection so we can safely overwrite the file.
	b.dbm.closeDB(dbName)

	if err := b.st.Get(filename, dbPath); err != nil {
		return fmt.Errorf("restore copy: %w", err)
	}

	// Remove stale WAL/SHM sidecars so the next open sees clean state.
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	return nil
}

// DeleteBackup removes a backup file.
func (b *BackupManager) DeleteBackup(dbName, filename string) error {
	if !backupFilenameRe.MatchString(filename) {
		return &httpError{http.StatusBadRequest, "invalid backup filename"}
	}
	ok, err := b.st.Exists(filename)
	if err != nil {
		return err
	}
	if !ok {
		return &httpError{http.StatusNotFound, "backup not found: " + filename}
	}
	return b.st.Delete(filename)
}

// BackupAll creates backups of every registered database plus registry.json.
func (b *BackupManager) BackupAll() ([]map[string]any, error) {
	var results []map[string]any
	for _, info := range b.dbm.list() {
		meta, err := b.CreateBackup(info.Name)
		if err != nil {
			slog.Warn("backup-all: failed", "db", info.Name, "err", err)
			continue
		}
		results = append(results, map[string]any{
			"database":   info.Name,
			"filename":   meta.Filename,
			"size_bytes": meta.SizeBytes,
			"created_at": meta.CreatedAt,
		})
	}

	// Also back up registry.json via the storage backend.
	regSrc := b.dbm.registryPath()
	if _, err := os.Stat(regSrc); err == nil {
		regFilename := backupFilename("registry")
		if size, err := b.st.Put(regFilename, regSrc); err == nil {
			results = append(results, map[string]any{
				"database":   "registry",
				"filename":   regFilename,
				"size_bytes": size,
				"created_at": createdAtFromFilename(regFilename),
			})
		}
	}

	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}

// rotateBackups deletes oldest backups when count exceeds BackupMax.
func (b *BackupManager) rotateBackups(dbName string) {
	metas, err := b.st.List(dbName + "_")
	if err != nil || len(metas) <= b.cfg.BackupMax {
		return
	}
	// List is newest-first; oldest are at the end.
	for _, m := range metas[b.cfg.BackupMax:] {
		if err := b.st.Delete(m.Filename); err != nil {
			slog.Warn("rotate: delete failed", "file", m.Filename, "err", err)
		}
	}
}

// startScheduler runs periodic backups if BACKUP_SCHEDULE is configured.
func (b *BackupManager) startScheduler(ctx context.Context) {
	interval, err := parseSchedule(b.cfg.BackupSchedule)
	if err != nil || interval <= 0 {
		return
	}
	slog.Info("backup scheduler started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			slog.Info("scheduled backup starting")
			if _, err := b.BackupAll(); err != nil {
				slog.Error("scheduled backup failed", "err", err)
			} else {
				slog.Info("scheduled backup complete")
			}
		case <-ctx.Done():
			return
		}
	}
}

// parseSchedule parses strings like "30s", "5m", "6h", "1d".
func parseSchedule(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid schedule: %q", s)
	}
	unit := s[len(s)-1]
	n, err := strconv.ParseFloat(s[:len(s)-1], 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid schedule: %q", s)
	}
	switch unit {
	case 's':
		return time.Duration(n * float64(time.Second)), nil
	case 'm':
		return time.Duration(n * float64(time.Minute)), nil
	case 'h':
		return time.Duration(n * float64(time.Hour)), nil
	case 'd':
		return time.Duration(n * float64(24*time.Hour)), nil
	default:
		return 0, fmt.Errorf("invalid schedule unit %q in %q", string(unit), s)
	}
}

// dbNameFromPath extracts a valid database name from a file path.
func dbNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if len(result) > 64 {
		result = result[:64]
	}
	if result == "" {
		result = "db"
	}
	return result
}
