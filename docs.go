package main

import (
	"encoding/json"
	"net/http"
)

func (s *Server) openAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildOpenAPISpec())
}

func (s *Server) docsUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>EasyDB API Docs</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  SwaggerUIBundle({
    url: "/openapi.json",
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
    layout: "BaseLayout",
    deepLinking: true,
  })
</script>
</body>
</html>`))
}

func buildOpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "EasyDB",
			"description": "Lightweight SQLite REST API server",
			"version":     "1.0.0",
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"ApiKeyAuth": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-API-Key",
				},
			},
			"schemas": map[string]any{
				"ColumnInfo": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cid":        map[string]any{"type": "integer"},
						"name":       map[string]any{"type": "string"},
						"type":       map[string]any{"type": "string"},
						"notnull":    map[string]any{"type": "integer"},
						"dflt_value": map[string]any{"nullable": true},
						"pk":         map[string]any{"type": "integer"},
					},
				},
				"BackupMeta": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filename":   map[string]any{"type": "string"},
						"size_bytes": map[string]any{"type": "integer"},
						"created_at": map[string]any{"type": "string"},
					},
				},
			},
		},
		"security": []any{map[string]any{"ApiKeyAuth": []any{}}},
		"paths":    buildPaths(),
	}
}

func buildPaths() map[string]any {
	json200 := func(desc string, schema map[string]any) map[string]any {
		return map[string]any{
			"200": map[string]any{
				"description": desc,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schema},
				},
			},
		}
	}
	obj := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	arr := func(items map[string]any) map[string]any {
		return map[string]any{"type": "array", "items": items}
	}
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	bol := map[string]any{"type": "boolean"}
	ref := func(name string) map[string]any {
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	body := func(schema map[string]any) map[string]any {
		return map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}
	param := func(name, in, desc string, required bool) map[string]any {
		return map[string]any{
			"name": name, "in": in,
			"description": desc,
			"required":    required,
			"schema":      str,
		}
	}
	dbParam := param("db", "path", "Database name", true)
	tableParam := param("table", "path", "Table name", true)
	rowIDParam := param("row_id", "path", "Primary key value", true)
	rowidParam := param("rowid", "path", "SQLite implicit rowid", true)
	filenameParam := param("filename", "path", "Backup filename", true)

	return map[string]any{
		"/health": map[string]any{
			"get": map[string]any{
				"summary":  "Health check",
				"tags":     []string{"System"},
				"security": []any{},
				"responses": json200("OK", obj(map[string]any{
					"status": str, "dev_mode": bol,
				})),
			},
		},
		"/api/databases": map[string]any{
			"get": map[string]any{
				"summary": "List databases",
				"tags":    []string{"Databases"},
				"responses": json200("List of databases", obj(map[string]any{
					"databases": arr(obj(map[string]any{
						"name": str, "path": str, "exists": bol, "size_bytes": num,
					})),
				})),
			},
			"post": map[string]any{
				"summary": "Register a database",
				"tags":    []string{"Databases"},
				"requestBody": body(obj(map[string]any{
					"name": str, "path": str,
				})),
				"responses": map[string]any{
					"201": map[string]any{"description": "Registered",
						"content": map[string]any{"application/json": map[string]any{
							"schema": obj(map[string]any{"name": str, "path": str}),
						}}},
				},
			},
		},
		"/api/databases/upload": map[string]any{
			"post": map[string]any{
				"summary": "Upload a SQLite file and register it",
				"tags":    []string{"Databases"},
				"parameters": []any{
					map[string]any{"name": "name", "in": "query", "required": true, "schema": str},
				},
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"multipart/form-data": map[string]any{
							"schema": obj(map[string]any{
								"file": map[string]any{"type": "string", "format": "binary"},
							}),
						},
					},
				},
				"responses": map[string]any{"201": map[string]any{"description": "Uploaded"}},
			},
		},
		"/api/databases/import": map[string]any{
			"post": map[string]any{
				"summary":     "Download a database from a URL and register it",
				"tags":        []string{"Databases"},
				"requestBody": body(obj(map[string]any{"name": str, "url": str})),
				"responses":   map[string]any{"201": map[string]any{"description": "Imported"}},
			},
		},
		"/api/databases/backup-all": map[string]any{
			"post": map[string]any{
				"summary": "Backup all registered databases",
				"tags":    []string{"Backups"},
				"responses": map[string]any{
					"201": map[string]any{"description": "Backups created"},
				},
			},
		},
		"/api/databases/{db}": map[string]any{
			"delete": map[string]any{
				"summary":    "Unregister a database",
				"tags":       []string{"Databases"},
				"parameters": []any{dbParam, map[string]any{"name": "delete_file", "in": "query", "schema": bol}},
				"responses":  json200("Unregistered", obj(map[string]any{"unregistered": str, "file_deleted": bol})),
			},
		},
		"/api/databases/{db}/info": map[string]any{
			"get": map[string]any{
				"summary":    "Get SQLite metadata",
				"tags":       []string{"Databases"},
				"parameters": []any{dbParam},
				"responses": json200("Metadata", obj(map[string]any{
					"sqlite_version": str, "journal_mode": str, "page_size": num, "encoding": str,
				})),
			},
		},
		"/api/databases/{db}/tables": map[string]any{
			"get": map[string]any{
				"summary":    "List all tables",
				"tags":       []string{"Tables"},
				"parameters": []any{dbParam},
				"responses": json200("Tables", obj(map[string]any{
					"tables": arr(obj(map[string]any{
						"name": str, "columns": arr(ref("ColumnInfo")), "row_count": num,
					})),
				})),
			},
		},
		"/api/databases/{db}/tables/{table}/schema": map[string]any{
			"get": map[string]any{
				"summary":    "Get table column schema",
				"tags":       []string{"Tables"},
				"parameters": []any{dbParam, tableParam},
				"responses":  json200("Schema", obj(map[string]any{"table": str, "columns": arr(ref("ColumnInfo"))})),
			},
		},
		"/api/databases/{db}/tables/{table}/ddl": map[string]any{
			"get": map[string]any{
				"summary":    "Get CREATE TABLE statement",
				"tags":       []string{"Tables"},
				"parameters": []any{dbParam, tableParam},
				"responses":  json200("DDL", obj(map[string]any{"table": str, "sql": str})),
			},
		},
		"/api/databases/{db}/tables/{table}/rows": map[string]any{
			"get": map[string]any{
				"summary":    "List rows (paginated)",
				"tags":       []string{"Rows"},
				"parameters": []any{dbParam, tableParam,
					map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 50, "minimum": 1, "maximum": 1000}},
					map[string]any{"name": "offset", "in": "query", "schema": num},
					map[string]any{"name": "order_by", "in": "query", "schema": str},
					map[string]any{"name": "order_dir", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"ASC", "DESC"}}},
				},
				"responses": json200("Rows", obj(map[string]any{
					"rows": arr(map[string]any{"type": "object"}), "total": num, "limit": num, "offset": num,
				})),
			},
			"post": map[string]any{
				"summary":     "Insert a row",
				"tags":        []string{"Rows"},
				"parameters":  []any{dbParam, tableParam},
				"requestBody": body(map[string]any{"type": "object"}),
				"responses":   map[string]any{"201": map[string]any{"description": "Inserted", "content": map[string]any{"application/json": map[string]any{"schema": obj(map[string]any{"lastrowid": num})}}}},
			},
		},
		"/api/databases/{db}/tables/{table}/rows/{row_id}": map[string]any{
			"get": map[string]any{
				"summary":    "Get row by primary key",
				"tags":       []string{"Rows"},
				"parameters": []any{dbParam, tableParam, rowIDParam},
				"responses":  json200("Row", map[string]any{"type": "object"}),
			},
			"put": map[string]any{
				"summary":     "Update row by primary key",
				"tags":        []string{"Rows"},
				"parameters":  []any{dbParam, tableParam, rowIDParam},
				"requestBody": body(map[string]any{"type": "object"}),
				"responses":   json200("Updated", obj(map[string]any{"updated": bol})),
			},
			"delete": map[string]any{
				"summary":    "Delete row by primary key",
				"tags":       []string{"Rows"},
				"parameters": []any{dbParam, tableParam, rowIDParam},
				"responses":  json200("Deleted", obj(map[string]any{"deleted": bol})),
			},
		},
		"/api/databases/{db}/tables/{table}/rows/rowid/{rowid}": map[string]any{
			"put": map[string]any{
				"summary":     "Update row by SQLite rowid",
				"tags":        []string{"Rows"},
				"parameters":  []any{dbParam, tableParam, rowidParam},
				"requestBody": body(map[string]any{"type": "object"}),
				"responses":   json200("Updated", obj(map[string]any{"updated": bol})),
			},
			"delete": map[string]any{
				"summary":    "Delete row by SQLite rowid",
				"tags":       []string{"Rows"},
				"parameters": []any{dbParam, tableParam, rowidParam},
				"responses":  json200("Deleted", obj(map[string]any{"deleted": bol})),
			},
		},
		"/api/databases/{db}/query": map[string]any{
			"post": map[string]any{
				"summary":    "Execute raw SQL",
				"tags":       []string{"Query"},
				"parameters": []any{dbParam},
				"requestBody": body(obj(map[string]any{
					"sql":    str,
					"params": map[string]any{"type": "array", "items": map[string]any{}},
				})),
				"responses": json200("Query result", map[string]any{
					"oneOf": []any{
						obj(map[string]any{"columns": arr(str), "rows": arr(map[string]any{"type": "object"}), "rowcount": num, "truncated": bol}),
						obj(map[string]any{"rowcount": num, "lastrowid": num}),
					},
				}),
			},
		},
		"/api/databases/{db}/backup": map[string]any{
			"post": map[string]any{
				"summary":    "Create a backup",
				"tags":       []string{"Backups"},
				"parameters": []any{dbParam},
				"responses": map[string]any{
					"201": map[string]any{"description": "Backup created", "content": map[string]any{"application/json": map[string]any{
						"schema": obj(map[string]any{"database": str, "filename": str, "size_bytes": num, "created_at": str}),
					}}},
				},
			},
		},
		"/api/databases/{db}/backups": map[string]any{
			"get": map[string]any{
				"summary":    "List backups",
				"tags":       []string{"Backups"},
				"parameters": []any{dbParam},
				"responses":  json200("Backups", obj(map[string]any{"backups": arr(ref("BackupMeta"))})),
			},
		},
		"/api/databases/{db}/backups/{filename}": map[string]any{
			"get": map[string]any{
				"summary":    "Download a backup file",
				"tags":       []string{"Backups"},
				"parameters": []any{dbParam, filenameParam},
				"responses": map[string]any{
					"200": map[string]any{"description": "SQLite file", "content": map[string]any{"application/x-sqlite3": map[string]any{}}},
				},
			},
			"delete": map[string]any{
				"summary":    "Delete a backup",
				"tags":       []string{"Backups"},
				"parameters": []any{dbParam, filenameParam},
				"responses":  json200("Deleted", obj(map[string]any{"deleted": str})),
			},
		},
		"/api/databases/{db}/backups/{filename}/restore": map[string]any{
			"post": map[string]any{
				"summary":    "Restore database from backup",
				"tags":       []string{"Backups"},
				"parameters": []any{dbParam, filenameParam},
				"responses":  json200("Restored", obj(map[string]any{"database": str, "restored_from": str})),
			},
		},
	}
}
