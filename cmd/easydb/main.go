package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var (
		dbPath  = flag.String("db", "", "SQLite file to auto-register on startup")
		host    = flag.String("host", "", "Bind address (overrides HOST env var)")
		port    = flag.String("port", "", "Port number (overrides PORT env var)")
		dataDir = flag.String("data-dir", "", "Data directory (overrides DATA_DIR env var)")
	)
	flag.Parse()

	cfg := loadConfig()
	if *host != "" {
		cfg.Host = *host
	}
	if *port != "" {
		cfg.Port = *port
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
		cfg.BackupDir = cfg.DataDir + "/backups"
	}
	if *dbPath != "" {
		cfg.InitialDB = *dbPath
	}
	cfg.ExploratoryMode = cfg.InitialDB != ""

	if !cfg.ExploratoryMode {
		if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
			slog.Error("create data dir", "err", err)
			os.Exit(1)
		}
	}

	dbm, err := newDBManager(cfg.DataDir, cfg.ExploratoryMode)
	if err != nil {
		slog.Error("init db manager", "err", err)
		os.Exit(1)
	}

	bkm, err := newBackupManager(cfg, dbm)
	if err != nil {
		slog.Error("init backup manager", "err", err)
		os.Exit(1)
	}

	if cfg.InitialDB != "" {
		name := dbNameFromPath(cfg.InitialDB)
		if err := dbm.register(name, cfg.InitialDB); err != nil {
			slog.Warn("auto-register db", "path", cfg.InitialDB, "err", err)
		} else {
			slog.Info("auto-registered db", "name", name, "path", cfg.InitialDB)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go bkm.startScheduler(ctx)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           newServer(cfg, dbm, bkm),
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	printBanner(cfg, dbm)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
