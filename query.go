package main

import (
	"net/http"
	"strings"
)

const maxQueryRows = 10_000

func (s *Server) executeQuery(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}

	var body struct {
		SQL    string `json:"sql"`
		Params []any  `json:"params"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.SQL == "" {
		writeError(w, http.StatusBadRequest, "sql is required")
		return
	}
	if body.Params == nil {
		body.Params = []any{}
	}

	// Route to Query or Exec based on statement type to get the right result shape.
	if isSelectLike(body.SQL) {
		rows, err := db.Query(body.SQL, body.Params...)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if handleErr(w, err) {
			return
		}

		// fetchmany +1 to detect truncation
		var result []map[string]any
		for rows.Next() && len(result) <= maxQueryRows {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				handleErr(w, err)
				return
			}
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				row[col] = vals[i]
			}
			result = append(result, row)
		}
		if err := rows.Err(); handleErr(w, err) {
			return
		}

		truncated := false
		if len(result) > maxQueryRows {
			result = result[:maxQueryRows]
			truncated = true
		}
		if result == nil {
			result = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"columns":   cols,
			"rows":      result,
			"rowcount":  len(result),
			"truncated": truncated,
		})
	} else {
		res, err := db.Exec(body.SQL, body.Params...)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rowcount, _ := res.RowsAffected()
		lastID, _ := res.LastInsertId()
		writeJSON(w, http.StatusOK, map[string]any{
			"rowcount":  rowcount,
			"lastrowid": lastID,
		})
	}
}

// isSelectLike returns true for statements that return rows.
func isSelectLike(sql string) bool {
	s := strings.ToUpper(strings.TrimSpace(sql))
	for _, prefix := range []string{"SELECT", "WITH", "EXPLAIN", "PRAGMA", "VALUES"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
