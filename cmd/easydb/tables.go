package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) listTables(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}

	// Collect table names first, then close the cursor before making nested queries.
	// With MaxOpenConns(1), holding a rows cursor while calling tableColumns deadlocks.
	nameRows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if handleErr(w, err) {
		return
	}
	var tableNames []string
	for nameRows.Next() {
		var name string
		if err := nameRows.Scan(&name); err != nil {
			nameRows.Close()
			handleErr(w, err)
			return
		}
		tableNames = append(tableNames, name)
	}
	nameRows.Close()
	if err := nameRows.Err(); handleErr(w, err) {
		return
	}

	type TableInfo struct {
		Name     string       `json:"name"`
		Columns  []ColumnInfo `json:"columns"`
		RowCount int64        `json:"row_count"`
	}

	var tables []TableInfo
	for _, name := range tableNames {
		cols, err := tableColumns(db, name)
		if err != nil {
			handleErr(w, err)
			return
		}
		var count int64
		db.QueryRow("SELECT count(*) FROM " + quoteID(name)).Scan(&count) //nolint:errcheck
		tables = append(tables, TableInfo{Name: name, Columns: cols, RowCount: count})
	}
	if tables == nil {
		tables = []TableInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

func (s *Server) tableSchema(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}
	cols, err := tableColumns(db, table)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"table": table, "columns": cols})
}

func (s *Server) tableDDL(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	var sqlDDL string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&sqlDDL)
	if err != nil {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"table": table, "sql": sqlDDL})
}

// --- Row CRUD ---

func (s *Server) listRows(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}

	// Parse pagination params
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1000 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	cols, err := tableColumns(db, table)
	if handleErr(w, err) {
		return
	}
	pk := pkColumn(cols)

	// Build ORDER BY clause (validated against known columns)
	orderBy := r.URL.Query().Get("order_by")
	orderDir := strings.ToUpper(r.URL.Query().Get("order_dir"))
	if orderDir != "DESC" {
		orderDir = "ASC"
	}
	var orderClause string
	if orderBy != "" {
		if orderBy == "_rowid_" {
			orderClause = " ORDER BY rowid " + orderDir
		} else if !columnExists(cols, orderBy) {
			writeError(w, http.StatusBadRequest, "unknown column: "+orderBy)
			return
		} else {
			orderClause = " ORDER BY " + quoteID(orderBy) + " " + orderDir
		}
	}

	// SELECT with rowid if no single-column PK
	selectCols := "*"
	if pk == "" {
		selectCols = "rowid AS _rowid_, *"
	}

	var total int64
	db.QueryRow("SELECT count(*) FROM " + quoteID(table)).Scan(&total) //nolint:errcheck

	query := fmt.Sprintf("SELECT %s FROM %s%s LIMIT ? OFFSET ?",
		selectCols, quoteID(table), orderClause)
	rows, err := db.Query(query, limit, offset)
	if handleErr(w, err) {
		return
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":   result,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) createRow(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}

	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "request body must not be empty")
		return
	}

	cols, vals := mapToColsVals(body)
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = placeholders[:len(placeholders)-1]

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = quoteID(c)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteID(table), strings.Join(quotedCols, ","), placeholders)
	res, err := db.Exec(sql, vals...)
	if handleErr(w, err) {
		return
	}
	lastID, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"lastrowid": lastID})
}

func (s *Server) getRow(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	pkVal := r.PathValue("pk")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}
	cols, err := tableColumns(db, table)
	if handleErr(w, err) {
		return
	}
	pk := pkColumn(cols)
	if pk == "" {
		writeError(w, http.StatusBadRequest, "table has no single-column primary key")
		return
	}

	rows, err := db.Query(
		fmt.Sprintf("SELECT * FROM %s WHERE %s=?", quoteID(table), quoteID(pk)),
		pkVal,
	)
	if handleErr(w, err) {
		return
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if handleErr(w, err) {
		return
	}
	if len(result) == 0 {
		writeError(w, http.StatusNotFound, "row not found")
		return
	}
	writeJSON(w, http.StatusOK, result[0])
}

func (s *Server) updateRow(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	pkVal := r.PathValue("pk")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}
	cols, err := tableColumns(db, table)
	if handleErr(w, err) {
		return
	}
	pk := pkColumn(cols)
	if pk == "" {
		writeError(w, http.StatusBadRequest, "table has no single-column primary key")
		return
	}

	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	delete(body, pk) // PK is read-only

	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	setCols, setVals := mapToColsVals(body)
	setParts := make([]string, len(setCols))
	for i, c := range setCols {
		setParts[i] = quoteID(c) + "=?"
	}

	sql := fmt.Sprintf("UPDATE %s SET %s WHERE %s=?",
		quoteID(table), strings.Join(setParts, ","), quoteID(pk))
	setVals = append(setVals, pkVal)
	if _, err := db.Exec(sql, setVals...); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) getRowByRowid(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	rowid := r.PathValue("rowid")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}

	rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s WHERE rowid=?", quoteID(table)), rowid)
	if handleErr(w, err) {
		return
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if handleErr(w, err) {
		return
	}
	if len(result) == 0 {
		writeError(w, http.StatusNotFound, "row not found")
		return
	}
	writeJSON(w, http.StatusOK, result[0])
}

func (s *Server) updateRowByRowid(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	rowid := r.PathValue("rowid")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}

	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	setCols, setVals := mapToColsVals(body)
	setParts := make([]string, len(setCols))
	for i, c := range setCols {
		setParts[i] = quoteID(c) + "=?"
	}
	sql := fmt.Sprintf("UPDATE %s SET %s WHERE rowid=?",
		quoteID(table), strings.Join(setParts, ","))
	setVals = append(setVals, rowid)
	if _, err := db.Exec(sql, setVals...); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) deleteRow(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	pkVal := r.PathValue("pk")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}
	cols, err := tableColumns(db, table)
	if handleErr(w, err) {
		return
	}
	pk := pkColumn(cols)
	if pk == "" {
		writeError(w, http.StatusBadRequest, "table has no single-column primary key")
		return
	}

	res, err := db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE %s=?", quoteID(table), quoteID(pk)),
		pkVal,
	)
	if handleErr(w, err) {
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "row not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) deleteRowByRowid(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	table := r.PathValue("table")
	rowid := r.PathValue("rowid")

	db, err := s.dbm.openDB(dbName)
	if handleErr(w, err) {
		return
	}
	ok, err := tableExists(db, table)
	if handleErr(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "table not found: "+table)
		return
	}

	res, err := db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE rowid=?", quoteID(table)),
		rowid,
	)
	if handleErr(w, err) {
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "row not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- helpers ---

// mapToColsVals splits a map into parallel column name and value slices.
func mapToColsVals(m map[string]any) ([]string, []any) {
	cols := make([]string, 0, len(m))
	vals := make([]any, 0, len(m))
	for k, v := range m {
		cols = append(cols, k)
		vals = append(vals, v)
	}
	return cols, vals
}

// columnExists checks if name is in cols.
func columnExists(cols []ColumnInfo, name string) bool {
	for _, c := range cols {
		if c.Name == name {
			return true
		}
	}
	return false
}
