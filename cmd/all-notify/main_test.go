package main

import (
	"testing"
	"time"
)

func TestParseConfigUsesEnvironmentDefaults(t *testing.T) {
	t.Setenv("ALL_NOTIFY_ADDR", ":19080")
	t.Setenv("ALL_NOTIFY_DATA_DIR", "/tmp/all-notify")
	t.Setenv("ALL_NOTIFY_SEND_TIMEOUT", "15s")
	t.Setenv("ALL_NOTIFY_LOG_MAX_BYTES", "2048")
	t.Setenv("ALL_NOTIFY_LOG_MAX_BACKUPS", "9")

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":19080" {
		t.Fatalf("Addr=%q", cfg.Addr)
	}
	if cfg.DataDir != "/tmp/all-notify" {
		t.Fatalf("DataDir=%q", cfg.DataDir)
	}
	if cfg.SendTimeout != 15*time.Second {
		t.Fatalf("SendTimeout=%s", cfg.SendTimeout)
	}
	if cfg.LogMaxBytes != 2048 {
		t.Fatalf("LogMaxBytes=%d", cfg.LogMaxBytes)
	}
	if cfg.LogMaxBackups != 9 {
		t.Fatalf("LogMaxBackups=%d", cfg.LogMaxBackups)
	}
}

func TestParseConfigFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("ALL_NOTIFY_ADDR", ":19080")
	t.Setenv("ALL_NOTIFY_DATA_DIR", "/tmp/all-notify")
	t.Setenv("ALL_NOTIFY_SEND_TIMEOUT", "15s")
	t.Setenv("ALL_NOTIFY_LOG_MAX_BYTES", "2048")
	t.Setenv("ALL_NOTIFY_LOG_MAX_BACKUPS", "9")

	cfg, err := parseConfig([]string{
		"-addr", ":18080",
		"-data-dir", "./data",
		"-send-timeout", "3s",
		"-log-max-bytes", "4096",
		"-log-max-backups", "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":18080" {
		t.Fatalf("Addr=%q", cfg.Addr)
	}
	if cfg.DataDir != "./data" {
		t.Fatalf("DataDir=%q", cfg.DataDir)
	}
	if cfg.SendTimeout != 3*time.Second {
		t.Fatalf("SendTimeout=%s", cfg.SendTimeout)
	}
	if cfg.LogMaxBytes != 4096 {
		t.Fatalf("LogMaxBytes=%d", cfg.LogMaxBytes)
	}
	if cfg.LogMaxBackups != 2 {
		t.Fatalf("LogMaxBackups=%d", cfg.LogMaxBackups)
	}
}
