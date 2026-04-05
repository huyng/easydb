package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// private IP ranges blocked for SSRF protection on URL import.
var privateRanges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
}

func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"databases": s.dbm.list()})
}

func (s *Server) registerDatabase(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.dbm.register(body.Name, body.Path); handleErr(w, err) {
		return
	}
	path, _ := s.dbm.getPath(body.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"name": body.Name, "path": path})
}

func (s *Server) uploadDatabase(w http.ResponseWriter, r *http.Request) {
	// 500 MB limit
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "parse multipart: "+err.Error())
		return
	}

	name := r.FormValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name query param required")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field: "+err.Error())
		return
	}
	defer file.Close()

	destPath := filepath.Join(s.cfg.DataDir, name+".db")
	if err := s.dbm.register(name, destPath); handleErr(w, err) {
		return
	}

	out, err := os.Create(destPath)
	if err != nil {
		s.dbm.unregister(name, false)
		writeError(w, http.StatusInternalServerError, "create file: "+err.Error())
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		s.dbm.unregister(name, true)
		writeError(w, http.StatusInternalServerError, "write file: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name, "path": destPath})
}

func (s *Server) importDatabase(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" || body.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if err := validateImportURL(body.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	destPath := filepath.Join(s.cfg.DataDir, body.Name+".db")
	if err := s.dbm.register(body.Name, destPath); handleErr(w, err) {
		return
	}

	resp, err := http.Get(body.URL) //nolint:gosec — URL validated above
	if err != nil {
		s.dbm.unregister(body.Name, false)
		writeError(w, http.StatusBadGateway, "fetch URL: "+err.Error())
		return
	}
	defer resp.Body.Close()

	out, err := os.Create(destPath)
	if err != nil {
		s.dbm.unregister(body.Name, false)
		writeError(w, http.StatusInternalServerError, "create file: "+err.Error())
		return
	}
	defer out.Close()

	limited := io.LimitReader(resp.Body, 500<<20)
	if _, err := io.Copy(out, limited); err != nil {
		s.dbm.unregister(body.Name, true)
		writeError(w, http.StatusInternalServerError, "download: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": body.Name, "path": destPath})
}

func (s *Server) unregisterDatabase(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	deleteFile := r.URL.Query().Get("delete_file") == "true"
	if err := s.dbm.unregister(dbName, deleteFile); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"unregistered": dbName,
		"file_deleted": deleteFile,
	})
}

func (s *Server) databaseInfo(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}

	info := map[string]any{}
	for _, q := range []struct {
		key string
		sql string
	}{
		{"sqlite_version", "SELECT sqlite_version()"},
		{"journal_mode", "PRAGMA journal_mode"},
		{"page_size", "PRAGMA page_size"},
		{"encoding", "PRAGMA encoding"},
	} {
		var val any
		if err := db.QueryRow(q.sql).Scan(&val); err == nil {
			info[q.key] = val
		}
	}
	writeJSON(w, http.StatusOK, info)
}

// validateImportURL ensures the URL is HTTP(S) and not pointing at a private host.
func validateImportURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}

	host := u.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		for _, cidr := range privateRanges {
			_, network, _ := net.ParseCIDR(cidr)
			if network != nil && network.Contains(ip) {
				return fmt.Errorf("URL resolves to a private/reserved IP address")
			}
		}
	}
	return nil
}
