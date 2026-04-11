package main

import (
	"database/sql"
	"fmt"
	"net/http"
)

type txStatement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

type txResult struct {
	RowCount  int64 `json:"rowcount"`
	LastRowID int64 `json:"lastrowid"`
}

func (s *Server) executeTransaction(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}

	var body struct {
		Statements []txStatement `json:"statements"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body.Statements) == 0 {
		writeError(w, http.StatusBadRequest, "statements is required")
		return
	}

	tx, err := db.Begin()
	if handleErr(w, err) {
		return
	}

	results, err := runTransaction(tx, body.Statements)
	if err != nil {
		tx.Rollback()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := tx.Commit(); handleErr(w, err) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// runTransaction executes each statement inside tx, returning one result per statement.
func runTransaction(tx *sql.Tx, stmts []txStatement) ([]txResult, error) {
	results := make([]txResult, 0, len(stmts))
	for i, stmt := range stmts {
		if stmt.Params == nil {
			stmt.Params = []any{}
		}
		res, err := tx.Exec(stmt.SQL, stmt.Params...)
		if err != nil {
			return nil, fmt.Errorf("statement %d: %w", i, err)
		}
		rowcount, _ := res.RowsAffected()
		lastrowid, _ := res.LastInsertId()
		results = append(results, txResult{RowCount: rowcount, LastRowID: lastrowid})
	}
	return results, nil
}
