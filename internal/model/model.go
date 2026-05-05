package model

import "time"

const (
	TargetTypeBark = "bark"
	TargetTypeNtfy = "ntfy"
	TargetTypeSMTP = "smtp"

	StatusSuccess = "success"
	StatusFailed  = "failed"
)

type Route struct {
	ID           int64     `json:"id"`
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	DefaultTitle string    `json:"default_title"`
	Enabled      bool      `json:"enabled"`
	TargetIDs    []int64   `json:"target_ids,omitempty"`
	Targets      []Target  `json:"targets,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Target struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Config    string    `json:"config"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Notification struct {
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	URL      string   `json:"url,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type SendLog struct {
	ID             string            `json:"id"`
	RouteID        int64             `json:"route_id"`
	RouteKey       string            `json:"route_key"`
	Title          string            `json:"title"`
	Message        string            `json:"message"`
	RequestMethod  string            `json:"request_method"`
	RequestBody    string            `json:"request_body,omitempty"`
	Status         string            `json:"status"`
	TotalTargets   int               `json:"total_targets"`
	SuccessTargets int               `json:"success_targets"`
	FailedTargets  int               `json:"failed_targets"`
	DurationMS     int64             `json:"duration_ms"`
	RemoteAddr     string            `json:"remote_addr"`
	CreatedAt      time.Time         `json:"created_at"`
	TargetLogs     []TargetSendLog   `json:"target_logs,omitempty"`
	Extra          map[string]string `json:"extra,omitempty"`
}

type TargetSendLog struct {
	ID         int64     `json:"id"`
	SendLogID  string    `json:"send_log_id"`
	TargetID   int64     `json:"target_id"`
	TargetName string    `json:"target_name"`
	TargetType string    `json:"target_type"`
	Status     string    `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
	Response   string    `json:"response,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Settings struct {
	LogRetentionDays int `json:"log_retention_days"`
	LogMaxRows       int `json:"log_max_rows"`
}

func DefaultSettings() Settings {
	return Settings{LogRetentionDays: 30, LogMaxRows: 100000}
}
