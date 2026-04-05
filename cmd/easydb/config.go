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
	BackupS3Bucket  string // BACKUP_S3_BUCKET — enables S3 backend when non-empty
	BackupS3Prefix  string // BACKUP_S3_PREFIX  (default: "easydb-backups/")
	BackupS3Region  string // AWS_REGION
	BackupS3Endpoint string // BACKUP_S3_ENDPOINT — optional, for MinIO / Cloudflare R2
}

func loadConfig() *Config {
	cfg := &Config{
		Host:           getenv("HOST", "127.0.0.1"),
		Port:           getenv("PORT", "8000"),
		DataDir:        getenv("DATA_DIR", "data"),
		AdminEnabled:   getenv("ADMIN_ENABLED", "true") == "true",
		BackupMax:      5,
		BackupSchedule: os.Getenv("BACKUP_SCHEDULE"),
		InitialDB:      os.Getenv("EASYDB_OPEN"),
	}

	// API keys
	if raw := os.Getenv("API_KEYS"); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				cfg.APIKeys = append(cfg.APIKeys, k)
			}
		}
	}

	// CORS origins
	if raw := getenv("CORS_ORIGINS", "*"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.CORSOrigins = append(cfg.CORSOrigins, o)
			}
		}
	}

	// Backup dir
	cfg.BackupDir = getenv("BACKUP_DIR", cfg.DataDir+"/backups")

	// Backup max count
	if raw := os.Getenv("BACKUP_MAX_COUNT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.BackupMax = n
		}
	}

	// S3 backup backend
	cfg.BackupS3Bucket   = os.Getenv("BACKUP_S3_BUCKET")
	cfg.BackupS3Prefix   = getenv("BACKUP_S3_PREFIX", "easydb-backups/")
	cfg.BackupS3Region   = getenv("AWS_REGION", "us-east-1")
	cfg.BackupS3Endpoint = os.Getenv("BACKUP_S3_ENDPOINT")

	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
