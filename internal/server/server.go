package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"all_notify/internal/model"
	"all_notify/internal/notify"
	"all_notify/internal/store"
)

type Config struct {
	Addr          string
	DataDir       string
	SendTimeout   time.Duration
	LogMaxBytes   int64
	LogMaxBackups int
}

type Server struct {
	cfg        Config
	store      *store.Store
	dispatcher *notify.Dispatcher
	logger     *log.Logger
	appLogPath string
}

func New(cfg Config, st *store.Store, logger *log.Logger, appLogPath string) *Server {
	return &Server{
		cfg:        cfg,
		store:      st,
		dispatcher: notify.NewDispatcher(cfg.SendTimeout),
		logger:     logger,
		appLogPath: appLogPath,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/static/app.css", s.css)
	mux.HandleFunc("/favicon.ico", s.favicon)
	mux.HandleFunc("/send/", s.send)
	mux.HandleFunc("/api/routes", s.routes)
	mux.HandleFunc("/api/routes/", s.routeByID)
	mux.HandleFunc("/api/targets", s.targets)
	mux.HandleFunc("/api/targets/", s.targetByID)
	mux.HandleFunc("/api/logs", s.logs)
	mux.HandleFunc("/api/logs/", s.logByID)
	mux.HandleFunc("/api/runtime-logs", s.runtimeLogs)
	mux.HandleFunc("/api/settings", s.settings)
	mux.HandleFunc("/", s.web)
	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Printf("%s %s %d %s remote=%s", r.Method, r.URL.Path, rec.status, time.Since(start), r.RemoteAddr)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.store.Health(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET 和 POST")
		return
	}
	key := strings.Trim(strings.TrimPrefix(r.URL.Path, "/send/"), "/")
	if key == "" {
		writeError(w, http.StatusNotFound, "通知配置不存在")
		return
	}

	route, err := s.store.GetRouteForSend(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "通知配置不存在或已禁用")
		return
	}
	if err != nil {
		s.logger.Printf("读取通知配置失败 key=%s err=%v", key, err)
		writeError(w, http.StatusInternalServerError, "读取通知配置失败")
		return
	}

	notification, rawBody, err := parseNotificationRequest(r, route.DefaultTitle)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(notification.Message) == "" {
		writeError(w, http.StatusBadRequest, "message/body/content 不能为空")
		return
	}
	if notification.Title == "" {
		notification.Title = route.Name
	}

	entry, code := s.dispatchAndLog(r.Context(), r.Method, rawBody, r.RemoteAddr, route, notification)
	writeJSON(w, code, entry)
}

func (s *Server) dispatchAndLog(ctx context.Context, method, rawBody, remoteAddr string, route model.Route, notification model.Notification) (model.SendLog, int) {
	start := time.Now()
	if len(route.Targets) == 0 {
		entry := model.SendLog{
			ID:            newRequestID(),
			RouteID:       route.ID,
			RouteKey:      route.Key,
			Title:         notification.Title,
			Message:       notification.Message,
			RequestMethod: method,
			RequestBody:   truncate(rawBody, 8000),
			Status:        model.StatusFailed,
			DurationMS:    time.Since(start).Milliseconds(),
			RemoteAddr:    remoteAddr,
			CreatedAt:     time.Now(),
			Extra:         map[string]string{"error": "没有启用的发送目标"},
		}
		if err := s.store.InsertSendLog(ctx, entry); err != nil {
			s.logger.Printf("写入发送日志失败 id=%s err=%v", entry.ID, err)
		}
		return entry, http.StatusBadGateway
	}
	results := s.dispatcher.SendAll(ctx, notification, route.Targets)
	requestID := newRequestID()
	createdAt := time.Now()
	success, failed := 0, 0
	targetLogs := make([]model.TargetSendLog, 0, len(results))
	for _, result := range results {
		if result.Status == model.StatusSuccess {
			success++
		} else {
			failed++
		}
		targetLogs = append(targetLogs, model.TargetSendLog{
			SendLogID:  requestID,
			TargetID:   result.TargetID,
			TargetName: result.TargetName,
			TargetType: result.TargetType,
			Status:     result.Status,
			DurationMS: result.DurationMS,
			Error:      result.Error,
			Response:   result.Response,
			CreatedAt:  createdAt,
		})
	}
	status := model.StatusSuccess
	if failed > 0 {
		status = model.StatusFailed
	}
	entry := model.SendLog{
		ID:             requestID,
		RouteID:        route.ID,
		RouteKey:       route.Key,
		Title:          notification.Title,
		Message:        notification.Message,
		RequestMethod:  method,
		RequestBody:    truncate(rawBody, 8000),
		Status:         status,
		TotalTargets:   len(results),
		SuccessTargets: success,
		FailedTargets:  failed,
		DurationMS:     time.Since(start).Milliseconds(),
		RemoteAddr:     remoteAddr,
		CreatedAt:      createdAt,
		TargetLogs:     targetLogs,
	}
	if err := s.store.InsertSendLog(ctx, entry); err != nil {
		s.logger.Printf("写入发送日志失败 id=%s err=%v", entry.ID, err)
	}
	if settings, err := s.store.Settings(ctx); err == nil {
		if err := s.store.PruneSendLogs(context.Background(), settings); err != nil {
			s.logger.Printf("裁剪发送日志失败: %v", err)
		}
	}

	code := http.StatusOK
	if failed > 0 {
		code = http.StatusBadGateway
	}
	return entry, code
}

func (s *Server) routes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		routes, err := s.store.ListRoutes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, routes)
	case http.MethodPost:
		route, err := decodeRouteRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, err := s.store.CreateRoute(r.Context(), route)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) routeByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := idAndActionFromPath(w, r.URL.Path, "/api/routes/")
	if !ok {
		return
	}
	if action == "test" {
		s.testRoute(w, r, id)
		return
	}
	if action != "" {
		writeError(w, http.StatusNotFound, "资源不存在")
		return
	}
	switch r.Method {
	case http.MethodGet:
		route, err := s.store.GetRoute(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, route)
	case http.MethodPut:
		route, err := decodeRouteRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := s.store.UpdateRoute(r.Context(), id, route)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteRoute(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) targets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		targets, err := s.store.ListTargets(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, targets)
	case http.MethodPost:
		target, err := decodeTargetRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := notify.ValidateTarget(target); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, err := s.store.CreateTarget(r.Context(), target)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) targetByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := idAndActionFromPath(w, r.URL.Path, "/api/targets/")
	if !ok {
		return
	}
	if action == "test" {
		s.testTarget(w, r, id)
		return
	}
	if action != "" {
		writeError(w, http.StatusNotFound, "资源不存在")
		return
	}
	switch r.Method {
	case http.MethodGet:
		target, err := s.store.GetTarget(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, target)
	case http.MethodPut:
		target, err := decodeTargetRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := notify.ValidateTarget(target); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := s.store.UpdateTarget(r.Context(), id, target)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteTarget(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) testRoute(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	route, err := s.store.GetRoute(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !route.Enabled {
		writeError(w, http.StatusBadRequest, "通知入口已禁用，不能测试")
		return
	}
	route, err = s.store.GetRouteForSend(r.Context(), route.Key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	notification, rawBody, err := parseOptionalNotificationRequest(r, route.DefaultTitle)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if notification.Title == "" {
		notification.Title = "测试通知: " + route.Name
	}
	if notification.Message == "" {
		notification.Message = "这是一条来自 All Notify 的通知入口测试消息。"
	}
	entry, code := s.dispatchAndLog(r.Context(), r.Method, rawBody, r.RemoteAddr, route, notification)
	writeJSON(w, code, entry)
}

func (s *Server) testTarget(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	target, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !target.Enabled {
		writeError(w, http.StatusBadRequest, "发送目标已禁用，不能测试")
		return
	}
	notification, rawBody, err := parseOptionalNotificationRequest(r, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if notification.Title == "" {
		notification.Title = "测试通知: " + target.Name
	}
	if notification.Message == "" {
		notification.Message = "这是一条来自 All Notify 的发送目标测试消息。"
	}
	route := model.Route{
		Key:     "target:" + strconv.FormatInt(target.ID, 10),
		Name:    "目标测试: " + target.Name,
		Enabled: true,
		Targets: []model.Target{target},
	}
	entry, code := s.dispatchAndLog(r.Context(), r.Method, rawBody, r.RemoteAddr, route, notification)
	writeJSON(w, code, entry)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	filter := store.LogFilter{
		RouteKey: r.URL.Query().Get("route_key"),
		Status:   r.URL.Query().Get("status"),
		Limit:    parseLimit(r.URL.Query().Get("limit"), 100),
	}
	logs, err := s.store.ListSendLogs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) logByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/logs/"), "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "日志 ID 不能为空")
		return
	}
	entry, err := s.store.GetSendLog(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) runtimeLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	lines, err := ReadRecentLogLines(s.appLogPath, parseLimit(r.URL.Query().Get("limit"), 300))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.Settings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var settings model.Settings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, "设置 JSON 无效")
			return
		}
		updated, err := s.store.UpdateSettings(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.store.PruneSendLogs(context.Background(), updated); err != nil {
			s.logger.Printf("裁剪发送日志失败: %v", err)
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func parseNotificationRequest(r *http.Request, defaultTitle string) (model.Notification, string, error) {
	n := model.Notification{Title: strings.TrimSpace(defaultTitle)}
	query := r.URL.Query()
	applyValues(&n, query)
	if r.Method == http.MethodGet {
		return n, query.Encode(), nil
	}

	body, err := readLimitedBody(r.Body, 1024*1024)
	if err != nil {
		return n, "", errors.New("请求体过大或读取失败")
	}
	raw := string(body)
	contentType := strings.ToLower(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	switch contentType {
	case "application/json":
		values, err := parseJSONValues(body)
		if err != nil {
			return n, raw, err
		}
		applyValues(&n, values)
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(raw)
		if err != nil {
			return n, raw, errors.New("表单内容无效")
		}
		applyValues(&n, values)
	case "text/plain", "":
		if strings.TrimSpace(raw) != "" {
			n.Message = raw
		}
	default:
		values, err := url.ParseQuery(raw)
		if err == nil && len(values) > 0 {
			applyValues(&n, values)
		} else {
			n.Message = raw
		}
	}
	applyValues(&n, query)
	return n, raw, nil
}

func parseOptionalNotificationRequest(r *http.Request, defaultTitle string) (model.Notification, string, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return model.Notification{Title: strings.TrimSpace(defaultTitle)}, "", nil
	}
	return parseNotificationRequest(r, defaultTitle)
}

func parseJSONValues(body []byte) (url.Values, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("JSON 请求体无效")
	}
	values := make(url.Values)
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			values.Set(key, typed)
		case []any:
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, fmt.Sprint(item))
			}
			values.Set(key, strings.Join(parts, ","))
		case nil:
		default:
			values.Set(key, fmt.Sprint(typed))
		}
	}
	return values, nil
}

func applyValues(n *model.Notification, values url.Values) {
	if value := first(values, "title"); value != "" {
		n.Title = value
	}
	if value := first(values, "message", "body", "content"); value != "" {
		n.Message = value
	}
	if value := first(values, "url", "click"); value != "" {
		n.URL = value
	}
	if value := first(values, "priority", "level"); value != "" {
		n.Priority = value
	}
	if value := first(values, "tags", "tag"); value != "" {
		n.Tags = splitCSV(value)
	}
}

func decodeRouteRequest(r *http.Request) (model.Route, error) {
	var route model.Route
	var raw struct {
		Key          string  `json:"key"`
		Name         string  `json:"name"`
		DefaultTitle string  `json:"default_title"`
		Enabled      *bool   `json:"enabled"`
		TargetIDs    []int64 `json:"target_ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&raw); err != nil {
		return route, errors.New("route JSON 无效")
	}
	route = model.Route{
		Key:          strings.Trim(strings.TrimSpace(raw.Key), "/"),
		Name:         strings.TrimSpace(raw.Name),
		DefaultTitle: strings.TrimSpace(raw.DefaultTitle),
		Enabled:      true,
		TargetIDs:    raw.TargetIDs,
	}
	if raw.Enabled != nil {
		route.Enabled = *raw.Enabled
	}
	if route.Key == "" || route.Name == "" {
		return route, errors.New("key 和 name 不能为空")
	}
	return route, nil
}

func decodeTargetRequest(r *http.Request) (model.Target, error) {
	var raw struct {
		Name    string          `json:"name"`
		Type    string          `json:"type"`
		Config  json.RawMessage `json:"config"`
		Enabled *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&raw); err != nil {
		return model.Target{}, errors.New("target JSON 无效")
	}
	target := model.Target{
		Name:    strings.TrimSpace(raw.Name),
		Type:    strings.ToLower(strings.TrimSpace(raw.Type)),
		Enabled: true,
	}
	if raw.Enabled != nil {
		target.Enabled = *raw.Enabled
	}
	if len(raw.Config) == 0 || string(raw.Config) == "null" {
		return target, errors.New("config 不能为空")
	}
	var configString string
	if err := json.Unmarshal(raw.Config, &configString); err == nil {
		target.Config = configString
	} else {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw.Config); err != nil {
			return target, errors.New("config JSON 无效")
		}
		target.Config = buf.String()
	}
	return target, nil
}

func idAndActionFromPath(w http.ResponseWriter, path, prefix string) (int64, string, bool) {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	idText, action, _ := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idText, 10, 64)
	if idText == "" || err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, "", false
	}
	return id, action, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "资源不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func first(values url.Values, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(values.Get(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("body too large")
	}
	return data, nil
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = webTemplate.Execute(w, map[string]string{"Title": "All Notify"})
}

func (s *Server) css(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(appCSS))
}

func (s *Server) favicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var webTemplate = template.Must(template.New("web").Parse(indexHTML))
