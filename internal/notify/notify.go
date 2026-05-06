package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"all_notify/internal/model"
)

const responseLimit = 4000

type Dispatcher struct {
	Client  *http.Client
	Timeout time.Duration
}

type Result struct {
	TargetID   int64
	TargetName string
	TargetType string
	Status     string
	DurationMS int64
	Error      string
	Response   string
}

type BarkConfig struct {
	ServerURL  string   `json:"server_url"`
	DeviceKey  string   `json:"device_key"`
	DeviceKeys []string `json:"device_keys"`
	Group      string   `json:"group"`
	Sound      string   `json:"sound"`
	Icon       string   `json:"icon"`
	Level      string   `json:"level"`
}

type NtfyConfig struct {
	ServerURL string   `json:"server_url"`
	Topic     string   `json:"topic"`
	Token     string   `json:"token"`
	Username  string   `json:"username"`
	Password  string   `json:"password"`
	Priority  string   `json:"priority"`
	Tags      []string `json:"tags"`
}

type SMTPConfig struct {
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	Security      string   `json:"security"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	From          string   `json:"from"`
	To            []string `json:"to"`
	CC            []string `json:"cc"`
	BCC           []string `json:"bcc"`
	SubjectPrefix string   `json:"subject_prefix"`
}

type BoardConfig struct {
	ServerURL string `json:"server_url"`
	BoardID   string `json:"board_id"`
	APIToken  string `json:"api_token"`
	Mode      string `json:"mode"`
}

func NewDispatcher(timeout time.Duration) *Dispatcher {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Dispatcher{
		Timeout: timeout,
		Client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: timeout,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

func (d *Dispatcher) SendAll(ctx context.Context, notification model.Notification, targets []model.Target) []Result {
	results := make([]Result, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		i, target := i, target
		results[i] = Result{
			TargetID:   target.ID,
			TargetName: target.Name,
			TargetType: target.Type,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			targetCtx, cancel := context.WithTimeout(ctx, d.Timeout)
			defer cancel()
			results[i] = d.sendOne(targetCtx, notification, target)
		}()
	}
	wg.Wait()
	return results
}

func (d *Dispatcher) sendOne(ctx context.Context, notification model.Notification, target model.Target) Result {
	start := time.Now()
	result := Result{
		TargetID:   target.ID,
		TargetName: target.Name,
		TargetType: target.Type,
	}

	var response string
	var err error
	switch target.Type {
	case model.TargetTypeBark:
		response, err = d.sendBark(ctx, notification, target.Config)
	case model.TargetTypeNtfy:
		response, err = d.sendNtfy(ctx, notification, target.Config)
	case model.TargetTypeSMTP:
		response, err = sendSMTP(ctx, notification, target.Config, d.Timeout)
	case model.TargetTypeBoard:
		response, err = d.sendBoard(ctx, notification, target.Config)
	default:
		err = fmt.Errorf("不支持的目标类型: %s", target.Type)
	}

	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = model.StatusFailed
		result.Error = err.Error()
	} else {
		result.Status = model.StatusSuccess
		result.Response = truncate(response, responseLimit)
	}
	return result
}

func ValidateTarget(target model.Target) error {
	if strings.TrimSpace(target.Name) == "" {
		return errors.New("目标名称不能为空")
	}
	switch target.Type {
	case model.TargetTypeBark:
		var cfg BarkConfig
		if err := decodeConfig(target.Config, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.DeviceKey) == "" && len(cfg.DeviceKeys) == 0 {
			return errors.New("Bark 目标必须配置 device_key 或 device_keys")
		}
	case model.TargetTypeNtfy:
		var cfg NtfyConfig
		if err := decodeConfig(target.Config, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.Topic) == "" {
			return errors.New("ntfy 目标必须配置 topic")
		}
	case model.TargetTypeSMTP:
		var cfg SMTPConfig
		if err := decodeConfig(target.Config, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.Host) == "" || cfg.Port <= 0 || strings.TrimSpace(cfg.From) == "" || len(cfg.To)+len(cfg.CC)+len(cfg.BCC) == 0 {
			return errors.New("SMTP 目标必须配置 host、port、from 和至少一个收件人")
		}
	case model.TargetTypeBoard:
		var cfg BoardConfig
		if err := decodeConfig(target.Config, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.ServerURL) == "" || strings.TrimSpace(cfg.BoardID) == "" || strings.TrimSpace(cfg.APIToken) == "" {
			return errors.New("公告板目标必须配置 server_url、board_id 和 api_token")
		}
		if _, err := normalizeBoardMode(cfg.Mode); err != nil {
			return err
		}
	default:
		return fmt.Errorf("不支持的目标类型: %s", target.Type)
	}
	return nil
}

func (d *Dispatcher) sendBark(ctx context.Context, notification model.Notification, raw string) (string, error) {
	var cfg BarkConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return "", err
	}
	serverURL := defaultString(cfg.ServerURL, "https://api.day.app")
	serverURL = strings.TrimRight(serverURL, "/")

	payload := map[string]any{
		"title": notification.Title,
		"body":  notification.Message,
	}
	if notification.URL != "" {
		payload["url"] = notification.URL
	}
	if cfg.Group != "" {
		payload["group"] = cfg.Group
	}
	if cfg.Sound != "" {
		payload["sound"] = cfg.Sound
	}
	if cfg.Icon != "" {
		payload["icon"] = cfg.Icon
	}
	if cfg.Level != "" {
		payload["level"] = cfg.Level
	} else if notification.Priority != "" {
		payload["level"] = notification.Priority
	}

	endpoint := serverURL
	if len(cfg.DeviceKeys) > 0 {
		payload["device_keys"] = cfg.DeviceKeys
		endpoint += "/push"
	} else {
		if strings.TrimSpace(cfg.DeviceKey) == "" {
			return "", errors.New("Bark device_key 不能为空")
		}
		endpoint += "/" + url.PathEscape(cfg.DeviceKey)
	}

	return d.postJSON(ctx, endpoint, payload, nil)
}

func (d *Dispatcher) sendNtfy(ctx context.Context, notification model.Notification, raw string) (string, error) {
	var cfg NtfyConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return "", errors.New("ntfy topic 不能为空")
	}
	serverURL := strings.TrimRight(defaultString(cfg.ServerURL, "https://ntfy.sh"), "/")
	endpoint := serverURL + "/" + url.PathEscape(cfg.Topic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(notification.Message))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if notification.Title != "" {
		req.Header.Set("Title", notification.Title)
	}
	if priority := defaultString(notification.Priority, cfg.Priority); priority != "" {
		req.Header.Set("Priority", priority)
	}
	tags := notification.Tags
	if len(tags) == 0 {
		tags = cfg.Tags
	}
	if len(tags) > 0 {
		req.Header.Set("Tags", strings.Join(tags, ","))
	}
	if notification.URL != "" {
		req.Header.Set("Click", notification.URL)
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	} else if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	return d.doHTTP(req)
}

func (d *Dispatcher) sendBoard(ctx context.Context, notification model.Notification, raw string) (string, error) {
	var cfg BoardConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return "", err
	}
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	boardID := strings.Trim(strings.TrimSpace(cfg.BoardID), "/")
	apiToken := strings.TrimSpace(cfg.APIToken)
	if serverURL == "" || boardID == "" || apiToken == "" {
		return "", errors.New("公告板 server_url、board_id 和 api_token 不能为空")
	}
	mode, err := normalizeBoardMode(cfg.Mode)
	if err != nil {
		return "", err
	}

	content := notification.Message
	if notification.URL != "" {
		content = strings.TrimSpace(content + "\n\n" + notification.URL)
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("公告板 content 不能为空")
	}

	endpoint := serverURL + "/api/update/" + url.PathEscape(boardID)
	payload := map[string]string{
		"action":  mode,
		"content": content,
	}
	return d.postJSON(ctx, endpoint, payload, map[string]string{"Authorization": "Bearer " + apiToken})
}

func (d *Dispatcher) postJSON(ctx context.Context, endpoint string, payload any, headers map[string]string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return d.doHTTP(req)
}

func (d *Dispatcher) doHTTP(req *http.Request) (string, error) {
	resp, err := d.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if readErr != nil {
		return "", readErr
	}
	text := string(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return text, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(text, 500))
	}
	return text, nil
}

func sendSMTP(ctx context.Context, notification model.Notification, raw string, timeout time.Duration) (string, error) {
	var cfg SMTPConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return "", err
	}
	if cfg.Port <= 0 {
		return "", errors.New("SMTP port 必须大于 0")
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return "", errors.New("SMTP host 不能为空")
	}

	addr := net.JoinHostPort(cfg.Host, strconvItoa(cfg.Port))
	dialer := net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if strings.EqualFold(cfg.Security, "tls") {
		conn, err = tls.DialWithDialer(&dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return "", err
	}
	defer client.Close()

	if strings.EqualFold(cfg.Security, "starttls") {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "", errors.New("SMTP 服务端不支持 STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return "", err
		}
	}

	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return "", err
		}
	}

	from := strings.TrimSpace(cfg.From)
	if from == "" {
		return "", errors.New("SMTP from 不能为空")
	}
	recipients := append(append([]string{}, cfg.To...), append(cfg.CC, cfg.BCC...)...)
	if len(recipients) == 0 {
		return "", errors.New("SMTP 收件人不能为空")
	}
	if err := client.Mail(from); err != nil {
		return "", err
	}
	for _, recipient := range recipients {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		if err := client.Rcpt(recipient); err != nil {
			return "", err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return "", err
	}
	if _, err := writer.Write(buildMailMessage(cfg, notification)); err != nil {
		writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	if err := client.Quit(); err != nil {
		return "", err
	}
	return "smtp sent", nil
}

func buildMailMessage(cfg SMTPConfig, notification model.Notification) []byte {
	header := make(textproto.MIMEHeader)
	header.Set("From", cfg.From)
	header.Set("To", strings.Join(cfg.To, ", "))
	if len(cfg.CC) > 0 {
		header.Set("Cc", strings.Join(cfg.CC, ", "))
	}
	subject := strings.TrimSpace(strings.TrimSpace(cfg.SubjectPrefix) + " " + notification.Title)
	if subject == "" {
		subject = "Notification"
	}
	header.Set("Subject", mime.QEncoding.Encode("utf-8", subject))
	header.Set("MIME-Version", "1.0")
	header.Set("Content-Type", `text/plain; charset="utf-8"`)
	header.Set("Content-Transfer-Encoding", "base64")

	var buf bytes.Buffer
	for key, values := range header {
		for _, value := range values {
			buf.WriteString(key)
			buf.WriteString(": ")
			buf.WriteString(value)
			buf.WriteString("\r\n")
		}
	}
	buf.WriteString("\r\n")
	body := notification.Message
	if notification.URL != "" {
		body += "\n\n" + notification.URL
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76])
		buf.WriteString("\r\n")
		encoded = encoded[76:]
	}
	buf.WriteString(encoded)
	buf.WriteString("\r\n")
	return buf.Bytes()
}

func decodeConfig[T any](raw string, target *T) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("目标配置不能为空")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("目标配置 JSON 无效: %w", err)
	}
	return nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func normalizeBoardMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "append":
		return "append", nil
	case "new", "overwrite", "replace":
		return "new", nil
	default:
		return "", errors.New("公告板 mode 只支持 append 或 new")
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

func strconvItoa(v int) string {
	return fmt.Sprintf("%d", v)
}
