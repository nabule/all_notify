package main

import (
	"context"
	"errors"
	"flag"
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
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatalf("启动参数无效: %v", err)
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

func parseConfig(args []string) (server.Config, error) {
	cfg := server.Config{
		Addr:          env("ALL_NOTIFY_ADDR", ":8080"),
		DataDir:       env("ALL_NOTIFY_DATA_DIR", "/data"),
		SendTimeout:   durationEnv("ALL_NOTIFY_SEND_TIMEOUT", 10*time.Second),
		LogMaxBytes:   int64Env("ALL_NOTIFY_LOG_MAX_BYTES", 10*1024*1024),
		LogMaxBackups: intEnv("ALL_NOTIFY_LOG_MAX_BACKUPS", 5),
	}

	flags := flag.NewFlagSet("all-notify", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP 监听地址，例如 :8080")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "数据和日志目录")
	flags.DurationVar(&cfg.SendTimeout, "send-timeout", cfg.SendTimeout, "单个发送目标超时时间，例如 10s")
	flags.Int64Var(&cfg.LogMaxBytes, "log-max-bytes", cfg.LogMaxBytes, "单个运行日志文件最大字节数")
	flags.IntVar(&cfg.LogMaxBackups, "log-max-backups", cfg.LogMaxBackups, "运行日志轮转保留文件数")
	if err := flags.Parse(args); err != nil {
		return server.Config{}, err
	}
	return cfg, nil
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
