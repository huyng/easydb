package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// newTestServer creates a Server backed by a temp directory with a fresh DBManager.
func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "easydb-test-*")
	if err != nil {
		t.Fatal(err)
	}
	dbm, err := newDBManager(dir)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	cfg := &Config{} // no API keys — auth passthrough
	srv := newServer(cfg, dbm, nil)
	return srv, func() { os.RemoveAll(dir) }
}

// mustRegisterDB registers a new database and creates a table with a primary key.
func mustRegisterDB(t *testing.T, srv *Server, dbName string) {
	t.Helper()
	// Register DB via API
	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"name":%q}`, dbName)
	req := httptest.NewRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(body))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register db: got %d, body: %s", rec.Code, rec.Body)
	}

	// Create table via raw query endpoint
	createSQL := `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`
	rec2 := httptest.NewRecorder()
	qbody := fmt.Sprintf(`{"sql":%q}`, createSQL)
	req2 := httptest.NewRequest(http.MethodPost, "/api/databases/"+dbName+"/query", bytes.NewBufferString(qbody))
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("create table: got %d, body: %s", rec2.Code, rec2.Body)
	}
}

func TestGetRowByPK(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	// Insert a row
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/databases/testdb/tables/items/rows",
		bytes.NewBufferString(`{"id":1,"name":"alice"}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("insert row: got %d, body: %s", rec.Code, rec.Body)
	}

	// GET by pk
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/databases/testdb/tables/items/rows/1", nil)
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get row: got %d, body: %s", rec2.Code, rec2.Body)
	}

	var row map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", row["name"])
	}
}

func TestGetRowByPK_NotFound(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/databases/testdb/tables/items/rows/999", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateRowByPK(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	// Insert
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/databases/testdb/tables/items/rows",
		bytes.NewBufferString(`{"id":2,"name":"bob"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("insert: got %d", rec.Code)
	}

	// Update via pk
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/databases/testdb/tables/items/rows/2",
		bytes.NewBufferString(`{"name":"bobby"}`))
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("update: got %d, body: %s", rec2.Code, rec2.Body)
	}

	// Verify
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/api/databases/testdb/tables/items/rows/2", nil))
	var row map[string]any
	json.NewDecoder(rec3.Body).Decode(&row)
	if row["name"] != "bobby" {
		t.Errorf("expected name=bobby, got %v", row["name"])
	}
}

func TestDeleteRowByPK(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	// Insert
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/databases/testdb/tables/items/rows",
		bytes.NewBufferString(`{"id":3,"name":"carol"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("insert: got %d", rec.Code)
	}

	// Delete via pk
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, "/api/databases/testdb/tables/items/rows/3", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete: got %d, body: %s", rec2.Code, rec2.Body)
	}

	// Confirm gone
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/api/databases/testdb/tables/items/rows/3", nil))
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec3.Code)
	}
}

func TestGetRowByRowid(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	// Insert a row and capture its rowid from the response
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/databases/testdb/tables/items/rows",
		bytes.NewBufferString(`{"id":10,"name":"dave"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("insert: got %d", rec.Code)
	}
	var inserted map[string]any
	json.NewDecoder(rec.Body).Decode(&inserted)
	rowid := fmt.Sprintf("%v", int64(inserted["lastrowid"].(float64)))

	// GET by rowid
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet,
		"/api/databases/testdb/tables/items/rows/rowid/"+rowid, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("get by rowid: got %d, body: %s", rec2.Code, rec2.Body)
	}
	var row map[string]any
	json.NewDecoder(rec2.Body).Decode(&row)
	if row["name"] != "dave" {
		t.Errorf("expected name=dave, got %v", row["name"])
	}
}

func TestGetRowByRowid_NotFound(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/databases/testdb/tables/items/rows/rowid/9999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestPKRouteDoesNotConflictWithRowid verifies the {pk} wildcard doesn't match
// the literal "rowid" segment (which has its own dedicated routes).
func TestPKRouteDoesNotConflictWithRowid(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()
	mustRegisterDB(t, srv, "testdb")

	// PUT .../rows/rowid/{rowid} must route to updateRowByRowid, not updateRow.
	// A table with no explicit PK would cause updateRow to return 400;
	// updateRowByRowid would return 400 only for missing body — use that to distinguish.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/databases/testdb/tables/items/rows/rowid/1",
		bytes.NewBufferString(`{"name":"test"}`))
	srv.ServeHTTP(rec, req)
	// Should reach updateRowByRowid (which works with rowid) — not a 404 routing error.
	if rec.Code == http.StatusNotFound {
		t.Fatalf("rowid route not found — {pk} wildcard may have swallowed it")
	}
}
