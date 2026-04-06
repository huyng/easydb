package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[92m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[96m"
	ansiGray   = "\033[90m"
)

// printBanner writes a startup summary to stdout. Output is coloured when
// stdout is a TTY and plain text otherwise (pipes, log collectors, etc.).
func printBanner(cfg *Config, dbm *DBManager) {
	color := isatty.IsTerminal(os.Stdout.Fd())

	c := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + ansiReset
	}

	addr := fmt.Sprintf("http://%s:%s", cfg.Host, cfg.Port)

	lines := []string{""}

	// ── Header ──────────────────────────────────────────────────────
	lines = append(lines,
		"  "+c(ansiBold+ansiGreen, "EasyDB")+"  "+c(ansiGray, "·")+"  "+c(ansiCyan, addr),
		"",
	)

	// ── Endpoints ───────────────────────────────────────────────────
	row := func(label, value string) string {
		return fmt.Sprintf("  %s  %s", c(ansiDim, pad(label, 9)), value)
	}

	if cfg.AdminEnabled {
		lines = append(lines, row("admin", c(ansiCyan, addr+"/admin/")))
	}
	lines = append(lines,
		row("docs", c(ansiCyan, addr+"/docs/api")),
		"",
	)

	// ── Config summary ──────────────────────────────────────────────
	if len(cfg.APIKeys) == 0 {
		lines = append(lines, row("auth", c(ansiYellow, "⚠  open-access mode — set API_KEYS to enable authentication")))
	} else {
		lines = append(lines, row("auth", fmt.Sprintf("%d API key(s) configured", len(cfg.APIKeys))))
	}

	lines = append(lines, row("data", cfg.DataDir))

	if cfg.BackupS3Bucket != "" {
		lines = append(lines, row("backups", fmt.Sprintf("s3://%s/%s  (max %d per db)", cfg.BackupS3Bucket, strings.TrimSuffix(cfg.BackupS3Prefix, "/"), cfg.BackupMax)))
	} else {
		lines = append(lines, row("backups", fmt.Sprintf("local › %s  (max %d per db)", cfg.BackupDir, cfg.BackupMax)))
	}

	if cfg.BackupSchedule != "" {
		lines = append(lines, row("schedule", "every "+cfg.BackupSchedule))
	}

	lines = append(lines, "")

	// ── Registered databases ─────────────────────────────────────────
	dbs := dbm.list()
	if len(dbs) == 0 {
		lines = append(lines, row("databases", c(ansiGray, "none — register via API or --db flag")))
	} else {
		lines = append(lines, "  "+c(ansiDim, "databases"))
		for _, db := range dbs {
			lines = append(lines, fmt.Sprintf("    %s  %s", c(ansiBold, db.Name), c(ansiGray, db.Path)))
		}
	}

	lines = append(lines,
		"",
		"  "+c(ansiGray, "Ctrl+C to stop"),
		"",
	)

	fmt.Println(strings.Join(lines, "\n"))
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
