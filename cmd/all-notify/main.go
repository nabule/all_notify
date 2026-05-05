package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"all_notify/internal/server"
	"all_notify/internal/store"
)

func main() {
	cfg := server.Config{
		Addr:          env("ALL_NOTIFY_ADDR", ":8080"),
		DataDir:       env("ALL_NOTIFY_DATA_DIR", "/data"),
		SendTimeout:   durationEnv("ALL_NOTIFY_SEND_TIMEOUT", 10*time.Second),
		LogMaxBytes:   int64Env("ALL_NOTIFY_LOG_MAX_BYTES", 10*1024*1024),
		LogMaxBackups: intEnv("ALL_NOTIFY_LOG_MAX_BACKUPS", 5),
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	appLogPath := filepath.Join(cfg.DataDir, "logs", "app.log")
	logger, closeLog, err := server.NewAppLogger(appLogPath, cfg.LogMaxBytes, cfg.LogMaxBackups)
	if err != nil {
		log.Fatalf("初始化运行日志失败: %v", err)
	}
	defer closeLog()

	dbPath := filepath.Join(cfg.DataDir, "all_notify.db")
	st, err := store.Open(dbPath)
	if err != nil {
		logger.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		logger.Fatalf("数据库迁移失败: %v", err)
	}

	app := server.New(cfg, st, logger, appLogPath)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Printf("all-notify started addr=%s data_dir=%s", cfg.Addr, cfg.DataDir)
		errc <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Printf("received signal %s, shutting down", sig)
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("服务退出: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("服务关闭失败: %v", err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

func intEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func int64Env(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
