package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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

const defaultWindowsServiceName = "AllNotify"

var windowsServiceName = defaultWindowsServiceName

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatalf("启动参数无效: %v", err)
	}

	handled, err := platformRun(cfg)
	if err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
	if handled {
		return
	}

	if err := runConsole(cfg); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}

func runConsole(cfg server.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runApp(ctx, cfg)
}

func runApp(ctx context.Context, cfg server.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	appLogPath := filepath.Join(cfg.DataDir, "logs", "app.log")
	logger, closeLog, err := server.NewAppLogger(appLogPath, cfg.LogMaxBytes, cfg.LogMaxBackups)
	if err != nil {
		return fmt.Errorf("初始化运行日志失败: %w", err)
	}
	defer closeLog()

	dbPath := filepath.Join(cfg.DataDir, "all_notify.db")
	st, err := store.Open(dbPath)
	if err != nil {
		logger.Printf("打开数据库失败: %v", err)
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		logger.Printf("数据库迁移失败: %v", err)
		return fmt.Errorf("数据库迁移失败: %w", err)
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

	select {
	case <-ctx.Done():
		logger.Printf("shutdown requested: %v", ctx.Err())
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("服务退出: %v", err)
			return fmt.Errorf("服务退出: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("服务关闭失败: %v", err)
		return fmt.Errorf("服务关闭失败: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Printf("服务退出: %v", err)
		return fmt.Errorf("服务退出: %w", err)
	}
	return nil
}

func parseConfig(args []string) (server.Config, error) {
	cfg := server.Config{
		Addr:          env("ALL_NOTIFY_ADDR", ":8080"),
		DataDir:       env("ALL_NOTIFY_DATA_DIR", "/data"),
		SendTimeout:   durationEnv("ALL_NOTIFY_SEND_TIMEOUT", 10*time.Second),
		LogMaxBytes:   int64Env("ALL_NOTIFY_LOG_MAX_BYTES", 10*1024*1024),
		LogMaxBackups: intEnv("ALL_NOTIFY_LOG_MAX_BACKUPS", 5),
	}
	windowsServiceName = env("ALL_NOTIFY_SERVICE_NAME", defaultWindowsServiceName)

	flags := flag.NewFlagSet("all-notify", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP 监听地址，例如 :8080")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "数据和日志目录")
	flags.DurationVar(&cfg.SendTimeout, "send-timeout", cfg.SendTimeout, "单个发送目标超时时间，例如 10s")
	flags.Int64Var(&cfg.LogMaxBytes, "log-max-bytes", cfg.LogMaxBytes, "单个运行日志文件最大字节数")
	flags.IntVar(&cfg.LogMaxBackups, "log-max-backups", cfg.LogMaxBackups, "运行日志轮转保留文件数")
	flags.StringVar(&windowsServiceName, "service-name", windowsServiceName, "Windows 服务名称，例如 AllNotify")
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
