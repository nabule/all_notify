package server

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rotatingWriter struct {
	path       string
	maxBytes   int64
	maxBackups int
	mu         sync.Mutex
	file       *os.File
}

func NewAppLogger(path string, maxBytes int64, maxBackups int) (*log.Logger, func() error, error) {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	if maxBackups <= 0 {
		maxBackups = 5
	}
	writer := &rotatingWriter{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := writer.open(); err != nil {
		return nil, nil, err
	}
	multi := io.MultiWriter(os.Stdout, writer)
	logger := log.New(multi, "", log.LstdFlags|log.Lmicroseconds|log.LUTC)
	return logger, writer.close, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openLocked(); err != nil {
			return 0, err
		}
	}
	if info, err := w.file.Stat(); err == nil && info.Size()+int64(len(p)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *rotatingWriter) open() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.openLocked()
}

func (w *rotatingWriter) openLocked() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	return nil
}

func (w *rotatingWriter) rotateLocked() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	backup := w.path + "." + time.Now().UTC().Format("20060102150405")
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, backup); err != nil {
			return err
		}
	}
	if err := w.pruneLocked(); err != nil {
		return err
	}
	return w.openLocked()
}

func (w *rotatingWriter) pruneLocked() error {
	pattern := w.path + ".*"
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	sort.Strings(files)
	for len(files) > w.maxBackups {
		if err := os.Remove(files[0]); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}

func (w *rotatingWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func ReadRecentLogLines(path string, limit int) ([]string, error) {
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
