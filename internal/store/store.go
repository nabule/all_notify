package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"all_notify/internal/model"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type LogFilter struct {
	RouteKey string
	Status   string
	Limit    int
}

func Open(path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absPath}
	dsn := u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS routes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			default_title TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS targets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			config TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS route_targets (
			route_id INTEGER NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
			PRIMARY KEY (route_id, target_id)
		)`,
		`CREATE TABLE IF NOT EXISTS send_logs (
			id TEXT PRIMARY KEY,
			route_id INTEGER NOT NULL,
			route_key TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			request_method TEXT NOT NULL,
			request_body TEXT NOT NULL,
			status TEXT NOT NULL,
			total_targets INTEGER NOT NULL,
			success_targets INTEGER NOT NULL,
			failed_targets INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			remote_addr TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS target_send_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			send_log_id TEXT NOT NULL REFERENCES send_logs(id) ON DELETE CASCADE,
			target_id INTEGER NOT NULL,
			target_name TEXT NOT NULL,
			target_type TEXT NOT NULL,
			status TEXT NOT NULL,
			duration_ms INTEGER NOT NULL,
			error TEXT NOT NULL,
			response TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_send_logs_created_at ON send_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_send_logs_route_key ON send_logs(route_key)`,
		`CREATE INDEX IF NOT EXISTS idx_target_send_logs_send_log_id ON target_send_logs(send_log_id)`,
	}
	for _, stmt := range schema {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	defaults := model.DefaultSettings()
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO app_settings(key, value) VALUES('log_retention_days', ?), ('log_max_rows', ?)`, strconv.Itoa(defaults.LogRetentionDays), strconv.Itoa(defaults.LogMaxRows)); err != nil {
		return err
	}
	return nil
}

func (s *Store) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) ListRoutes(ctx context.Context) ([]model.Route, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, default_title, enabled, created_at, updated_at FROM routes ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}

	routes := make([]model.Route, 0)
	for rows.Next() {
		route, err := scanRoute(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for i := range routes {
		targetIDs, err := s.getRouteTargetIDs(ctx, routes[i].ID)
		if err != nil {
			return nil, err
		}
		routes[i].TargetIDs = targetIDs
	}
	return routes, nil
}

func (s *Store) GetRoute(ctx context.Context, id int64) (model.Route, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, key, name, default_title, enabled, created_at, updated_at FROM routes WHERE id = ?`, id)
	route, err := scanRoute(row)
	if err != nil {
		return model.Route{}, normalizeNotFound(err)
	}
	targetIDs, err := s.getRouteTargetIDs(ctx, route.ID)
	if err != nil {
		return model.Route{}, err
	}
	route.TargetIDs = targetIDs
	return route, nil
}

func (s *Store) GetRouteForSend(ctx context.Context, key string) (model.Route, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, key, name, default_title, enabled, created_at, updated_at FROM routes WHERE key = ?`, key)
	route, err := scanRoute(row)
	if err != nil {
		return model.Route{}, normalizeNotFound(err)
	}
	if !route.Enabled {
		return model.Route{}, ErrNotFound
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.type, t.config, t.enabled, t.created_at, t.updated_at
		FROM targets t
		JOIN route_targets rt ON rt.target_id = t.id
		WHERE rt.route_id = ? AND t.enabled = 1
		ORDER BY t.id`, route.ID)
	if err != nil {
		return model.Route{}, err
	}
	defer rows.Close()

	for rows.Next() {
		target, err := scanTarget(rows)
		if err != nil {
			return model.Route{}, err
		}
		route.Targets = append(route.Targets, target)
		route.TargetIDs = append(route.TargetIDs, target.ID)
	}
	return route, rows.Err()
}

func (s *Store) CreateRoute(ctx context.Context, route model.Route) (model.Route, error) {
	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Route{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO routes(key, name, default_title, enabled, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
		route.Key, route.Name, route.DefaultTitle, boolInt(route.Enabled), now, now)
	if err != nil {
		return model.Route{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Route{}, err
	}
	if err := replaceRouteTargets(ctx, tx, id, route.TargetIDs); err != nil {
		return model.Route{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Route{}, err
	}
	return s.GetRoute(ctx, id)
}

func (s *Store) UpdateRoute(ctx context.Context, id int64, route model.Route) (model.Route, error) {
	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Route{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE routes SET key = ?, name = ?, default_title = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		route.Key, route.Name, route.DefaultTitle, boolInt(route.Enabled), now, id)
	if err != nil {
		return model.Route{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Route{}, err
	}
	if affected == 0 {
		return model.Route{}, ErrNotFound
	}
	if err := replaceRouteTargets(ctx, tx, id, route.TargetIDs); err != nil {
		return model.Route{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Route{}, err
	}
	return s.GetRoute(ctx, id)
}

func (s *Store) DeleteRoute(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM routes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (s *Store) ListTargets(ctx context.Context) ([]model.Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, config, enabled, created_at, updated_at FROM targets ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]model.Target, 0)
	for rows.Next() {
		target, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s *Store) GetTarget(ctx context.Context, id int64) (model.Target, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, type, config, enabled, created_at, updated_at FROM targets WHERE id = ?`, id)
	target, err := scanTarget(row)
	return target, normalizeNotFound(err)
}

func (s *Store) CreateTarget(ctx context.Context, target model.Target) (model.Target, error) {
	now := nowText()
	res, err := s.db.ExecContext(ctx, `INSERT INTO targets(name, type, config, enabled, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
		target.Name, target.Type, target.Config, boolInt(target.Enabled), now, now)
	if err != nil {
		return model.Target{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Target{}, err
	}
	return s.GetTarget(ctx, id)
}

func (s *Store) UpdateTarget(ctx context.Context, id int64, target model.Target) (model.Target, error) {
	now := nowText()
	res, err := s.db.ExecContext(ctx, `UPDATE targets SET name = ?, type = ?, config = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		target.Name, target.Type, target.Config, boolInt(target.Enabled), now, id)
	if err != nil {
		return model.Target{}, err
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return model.Target{}, err
	}
	return s.GetTarget(ctx, id)
}

func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM targets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (s *Store) InsertSendLog(ctx context.Context, logEntry model.SendLog) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	createdAt := timeText(logEntry.CreatedAt)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO send_logs(id, route_id, route_key, title, message, request_method, request_body, status, total_targets, success_targets, failed_targets, duration_ms, remote_addr, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		logEntry.ID, logEntry.RouteID, logEntry.RouteKey, logEntry.Title, logEntry.Message, logEntry.RequestMethod, logEntry.RequestBody,
		logEntry.Status, logEntry.TotalTargets, logEntry.SuccessTargets, logEntry.FailedTargets, logEntry.DurationMS, logEntry.RemoteAddr, createdAt); err != nil {
		return err
	}
	for _, targetLog := range logEntry.TargetLogs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO target_send_logs(send_log_id, target_id, target_name, target_type, status, duration_ms, error, response, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			logEntry.ID, targetLog.TargetID, targetLog.TargetName, targetLog.TargetType, targetLog.Status, targetLog.DurationMS,
			targetLog.Error, targetLog.Response, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSendLogs(ctx context.Context, filter LogFilter) ([]model.SendLog, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var args []any
	where := []string{"1 = 1"}
	if filter.RouteKey != "" {
		where = append(where, "route_key = ?")
		args = append(args, filter.RouteKey)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`SELECT id, route_id, route_key, title, message, request_method, request_body, status, total_targets, success_targets, failed_targets, duration_ms, remote_addr, created_at FROM send_logs WHERE %s ORDER BY created_at DESC LIMIT ?`, strings.Join(where, " AND "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]model.SendLog, 0)
	for rows.Next() {
		entry, err := scanSendLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func (s *Store) GetSendLog(ctx context.Context, id string) (model.SendLog, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, route_id, route_key, title, message, request_method, request_body, status, total_targets, success_targets, failed_targets, duration_ms, remote_addr, created_at FROM send_logs WHERE id = ?`, id)
	entry, err := scanSendLog(row)
	if err != nil {
		return model.SendLog{}, normalizeNotFound(err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, send_log_id, target_id, target_name, target_type, status, duration_ms, error, response, created_at FROM target_send_logs WHERE send_log_id = ? ORDER BY id`, id)
	if err != nil {
		return model.SendLog{}, err
	}
	defer rows.Close()

	for rows.Next() {
		targetLog, err := scanTargetSendLog(rows)
		if err != nil {
			return model.SendLog{}, err
		}
		entry.TargetLogs = append(entry.TargetLogs, targetLog)
	}
	return entry, rows.Err()
}

func (s *Store) Settings(ctx context.Context) (model.Settings, error) {
	settings := model.DefaultSettings()
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		switch key {
		case "log_retention_days":
			settings.LogRetentionDays = n
		case "log_max_rows":
			settings.LogMaxRows = n
		}
	}
	return settings, rows.Err()
}

func (s *Store) UpdateSettings(ctx context.Context, settings model.Settings) (model.Settings, error) {
	if settings.LogRetentionDays <= 0 {
		settings.LogRetentionDays = model.DefaultSettings().LogRetentionDays
	}
	if settings.LogMaxRows <= 0 {
		settings.LogMaxRows = model.DefaultSettings().LogMaxRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Settings{}, err
	}
	defer tx.Rollback()
	for key, value := range map[string]int{
		"log_retention_days": settings.LogRetentionDays,
		"log_max_rows":       settings.LogMaxRows,
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, strconv.Itoa(value)); err != nil {
			return model.Settings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Settings{}, err
	}
	return s.Settings(ctx)
}

func (s *Store) PruneSendLogs(ctx context.Context, settings model.Settings) error {
	if settings.LogRetentionDays > 0 {
		cutoff := timeText(time.Now().AddDate(0, 0, -settings.LogRetentionDays))
		if _, err := s.db.ExecContext(ctx, `DELETE FROM send_logs WHERE created_at < ?`, cutoff); err != nil {
			return err
		}
	}
	if settings.LogMaxRows > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM send_logs WHERE id IN (
			SELECT id FROM send_logs ORDER BY created_at DESC LIMIT -1 OFFSET ?
		)`, settings.LogMaxRows)
		return err
	}
	return nil
}

func (s *Store) getRouteTargetIDs(ctx context.Context, routeID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT target_id FROM route_targets WHERE route_id = ? ORDER BY target_id`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func replaceRouteTargets(ctx context.Context, tx *sql.Tx, routeID int64, targetIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM route_targets WHERE route_id = ?`, routeID); err != nil {
		return err
	}
	seen := make(map[int64]bool)
	for _, targetID := range targetIDs {
		if targetID <= 0 || seen[targetID] {
			continue
		}
		seen[targetID] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO route_targets(route_id, target_id) VALUES(?, ?)`, routeID, targetID); err != nil {
			return err
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRoute(row scanner) (model.Route, error) {
	var route model.Route
	var enabled int
	var createdAt, updatedAt string
	err := row.Scan(&route.ID, &route.Key, &route.Name, &route.DefaultTitle, &enabled, &createdAt, &updatedAt)
	if err != nil {
		return route, err
	}
	route.Enabled = enabled == 1
	route.CreatedAt = parseTime(createdAt)
	route.UpdatedAt = parseTime(updatedAt)
	return route, nil
}

func scanTarget(row scanner) (model.Target, error) {
	var target model.Target
	var enabled int
	var createdAt, updatedAt string
	err := row.Scan(&target.ID, &target.Name, &target.Type, &target.Config, &enabled, &createdAt, &updatedAt)
	if err != nil {
		return target, err
	}
	target.Enabled = enabled == 1
	target.CreatedAt = parseTime(createdAt)
	target.UpdatedAt = parseTime(updatedAt)
	return target, nil
}

func scanSendLog(row scanner) (model.SendLog, error) {
	var entry model.SendLog
	var createdAt string
	err := row.Scan(&entry.ID, &entry.RouteID, &entry.RouteKey, &entry.Title, &entry.Message, &entry.RequestMethod, &entry.RequestBody,
		&entry.Status, &entry.TotalTargets, &entry.SuccessTargets, &entry.FailedTargets, &entry.DurationMS, &entry.RemoteAddr, &createdAt)
	if err != nil {
		return entry, err
	}
	entry.CreatedAt = parseTime(createdAt)
	return entry, nil
}

func scanTargetSendLog(row scanner) (model.TargetSendLog, error) {
	var entry model.TargetSendLog
	var createdAt string
	err := row.Scan(&entry.ID, &entry.SendLogID, &entry.TargetID, &entry.TargetName, &entry.TargetType, &entry.Status,
		&entry.DurationMS, &entry.Error, &entry.Response, &createdAt)
	if err != nil {
		return entry, err
	}
	entry.CreatedAt = parseTime(createdAt)
	return entry, nil
}

func normalizeNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func rowsAffectedOrNotFound(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nowText() string {
	return timeText(time.Now())
}

func timeText(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
