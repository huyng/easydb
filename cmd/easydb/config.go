package main

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all server configuration, sourced from environment variables.
type Config struct {
	Host            string
	Port            string
	DataDir         string
	APIKeys         []string
	AdminEnabled    bool
	CORSOrigins     []string
	BackupDir       string
	BackupMax       int
	BackupSchedule  string
	InitialDB       string
	ExploratoryMode bool   // true when --db is set; skips data dir creation until needed
	BackupS3Bucket   string // EASYDB_S3_BUCKET — enables S3 backend when non-empty
	BackupS3Prefix   string // EASYDB_S3_PREFIX  (default: "easydb-backups/")
	BackupS3Region   string // EASYDB_S3_REGION
	BackupS3Endpoint string // EASYDB_S3_ENDPOINT — optional, for MinIO / Cloudflare R2
}

func loadConfig() *Config {
	cfg := &Config{
		Host:           getenv("EASYDB_HOST", "127.0.0.1"),
		Port:           getenv("EASYDB_PORT", "8000"),
		DataDir:        getenv("EASYDB_DATA_DIR", "data"),
		AdminEnabled:   getenv("EASYDB_ADMIN_ENABLED", "true") == "true",
		BackupMax:      5,
		BackupSchedule: os.Getenv("EASYDB_BACKUP_SCHEDULE"),
		InitialDB:      os.Getenv("EASYDB_OPEN"),
	}

	// API keys
	if raw := os.Getenv("EASYDB_API_KEYS"); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				cfg.APIKeys = append(cfg.APIKeys, k)
			}
		}
	}

	// CORS origins
	if raw := getenv("EASYDB_CORS_ORIGINS", "*"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.CORSOrigins = append(cfg.CORSOrigins, o)
			}
		}
	}

	// Backup dir
	cfg.BackupDir = getenv("EASYDB_BACKUP_DIR", cfg.DataDir+"/backups")

	// Backup max count
	if raw := os.Getenv("EASYDB_BACKUP_MAX_COUNT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.BackupMax = n
		}
	}

	// S3 backup backend
	cfg.BackupS3Bucket   = os.Getenv("EASYDB_S3_BUCKET")
	cfg.BackupS3Prefix   = getenv("EASYDB_S3_PREFIX", "easydb-backups/")
	cfg.BackupS3Region   = getenv("EASYDB_S3_REGION", "us-east-1")
	cfg.BackupS3Endpoint = os.Getenv("EASYDB_S3_ENDPOINT")

	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
