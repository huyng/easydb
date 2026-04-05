package main

import (
	"net/http"
)

func (s *Server) backupAll(w http.ResponseWriter, r *http.Request) {
	results, err := s.bkm.BackupAll()
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"backups": results})
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	meta, err := s.bkm.CreateBackup(dbName)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"database":   dbName,
		"filename":   meta.Filename,
		"size_bytes": meta.SizeBytes,
		"created_at": meta.CreatedAt,
	})
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	// Validate db exists
	if _, err := s.dbm.getPath(dbName); handleErr(w, err) {
		return
	}
	metas, err := s.bkm.ListBackups(dbName)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": metas})
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	filename := r.PathValue("filename")
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := s.bkm.StreamBackup(dbName, filename, w); handleErr(w, err) {
		return
	}
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	filename := r.PathValue("filename")
	if err := s.bkm.RestoreBackup(dbName, filename); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"database":     dbName,
		"restored_from": filename,
	})
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	filename := r.PathValue("filename")
	if err := s.bkm.DeleteBackup(dbName, filename); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": filename})
}

