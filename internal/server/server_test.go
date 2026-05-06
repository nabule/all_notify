package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"all_notify/internal/model"
	"all_notify/internal/store"
)

func TestParseNotificationRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		url         string
		contentType string
		body        string
		wantTitle   string
		wantMessage string
		wantURL     string
		wantTags    []string
	}{
		{
			name:        "get query aliases",
			method:      http.MethodGet,
			url:         "/send/test?title=T&body=B&url=https://example.com&tags=a,b",
			wantTitle:   "T",
			wantMessage: "B",
			wantURL:     "https://example.com",
			wantTags:    []string{"a", "b"},
		},
		{
			name:        "post json",
			method:      http.MethodPost,
			url:         "/send/test",
			contentType: "application/json",
			body:        `{"title":"T","message":"M","tags":["x","y"]}`,
			wantTitle:   "T",
			wantMessage: "M",
			wantTags:    []string{"x", "y"},
		},
		{
			name:        "post form",
			method:      http.MethodPost,
			url:         "/send/test",
			contentType: "application/x-www-form-urlencoded",
			body:        "title=T&content=C",
			wantTitle:   "T",
			wantMessage: "C",
		},
		{
			name:        "post text with query title",
			method:      http.MethodPost,
			url:         "/send/test?title=T",
			contentType: "text/plain",
			body:        "plain message",
			wantTitle:   "T",
			wantMessage: "plain message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			got, _, err := parseNotificationRequest(req, "default")
			if err != nil {
				t.Fatalf("parseNotificationRequest() error = %v", err)
			}
			if got.Title != tt.wantTitle || got.Message != tt.wantMessage || got.URL != tt.wantURL {
				t.Fatalf("got title=%q message=%q url=%q", got.Title, got.Message, got.URL)
			}
			if strings.Join(got.Tags, ",") != strings.Join(tt.wantTags, ",") {
				t.Fatalf("got tags=%v want=%v", got.Tags, tt.wantTags)
			}
		})
	}
}

func TestSendAPIReadsCurrentConfigAndWritesLogs(t *testing.T) {
	var received bytes.Buffer
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device-1" {
			t.Fatalf("unexpected backend path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected backend method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		received.Write(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	targetConfig, _ := json.Marshal(map[string]any{
		"server_url": backend.URL,
		"device_key": "device-1",
	})
	target, err := st.CreateTarget(context.Background(), model.Target{
		Name:    "bark",
		Type:    model.TargetTypeBark,
		Config:  string(targetConfig),
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoute(context.Background(), model.Route{
		Key:       "alert",
		Name:      "Alert",
		Enabled:   true,
		TargetIDs: []int64{target.ID},
	}); err != nil {
		t.Fatal(err)
	}

	appLog := filepath.Join(tempDir, "app.log")
	app := New(Config{SendTimeout: 2 * time.Second}, st, log.New(io.Discard, "", 0), appLog)
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	res, err := http.Post(server.URL+"/send/alert", "application/json", strings.NewReader(`{"title":"Disk","message":"Full"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	if !strings.Contains(received.String(), `"body":"Full"`) {
		t.Fatalf("backend did not receive Bark JSON body: %s", received.String())
	}

	logs, err := st.ListSendLogs(context.Background(), store.LogFilter{RouteKey: "alert", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != model.StatusSuccess || logs[0].SuccessTargets != 1 {
		t.Fatalf("unexpected logs: %+v", logs)
	}
	detail, err := st.GetSendLog(context.Background(), logs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.TargetLogs) != 1 || detail.TargetLogs[0].Status != model.StatusSuccess {
		t.Fatalf("unexpected target logs: %+v", detail.TargetLogs)
	}
}

func TestConfigAPISavesTargetsAndRoutes(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	app := New(Config{SendTimeout: 2 * time.Second}, st, log.New(io.Discard, "", 0), filepath.Join(tempDir, "app.log"))
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	targetResp, err := http.Post(server.URL+"/api/targets", "application/json", strings.NewReader(`{
		"name": "ui bark",
		"type": "bark",
		"enabled": true,
		"config": {"server_url":"http://127.0.0.1:1","device_key":"x"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	defer targetResp.Body.Close()
	if targetResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(targetResp.Body)
		t.Fatalf("target save status=%d body=%s", targetResp.StatusCode, body)
	}
	var target model.Target
	if err := json.NewDecoder(targetResp.Body).Decode(&target); err != nil {
		t.Fatal(err)
	}
	if target.ID == 0 || target.Name != "ui bark" {
		t.Fatalf("target was not saved: %+v", target)
	}

	routePayload := `{"key":"ui-route","name":"UI Route","enabled":true,"target_ids":[` + strconv.FormatInt(target.ID, 10) + `]}`
	routeResp, err := http.Post(server.URL+"/api/routes", "application/json", strings.NewReader(routePayload))
	if err != nil {
		t.Fatal(err)
	}
	defer routeResp.Body.Close()
	if routeResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(routeResp.Body)
		t.Fatalf("route save status=%d body=%s", routeResp.StatusCode, body)
	}
	var route model.Route
	if err := json.NewDecoder(routeResp.Body).Decode(&route); err != nil {
		t.Fatal(err)
	}
	if route.ID == 0 || route.Key != "ui-route" || len(route.TargetIDs) != 1 || route.TargetIDs[0] != target.ID {
		t.Fatalf("route was not saved with target ids: %+v", route)
	}

	listResp, err := http.Get(server.URL + "/api/routes")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("route list status=%d body=%s", listResp.StatusCode, body)
	}
	var routes []model.Route
	if err := json.NewDecoder(listResp.Body).Decode(&routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Key != "ui-route" || len(routes[0].TargetIDs) != 1 {
		t.Fatalf("saved route missing from list: %+v", routes)
	}
}

func TestListAPIsReturnEmptyArrays(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	app := New(Config{SendTimeout: 2 * time.Second}, st, log.New(io.Discard, "", 0), filepath.Join(tempDir, "app.log"))
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	for _, path := range []string{"/api/routes", "/api/targets", "/api/logs?limit=20"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, body)
		}
		if strings.TrimSpace(string(body)) != "[]" {
			t.Fatalf("%s returned %s, want []", path, body)
		}
	}
}

func TestConfigTestAPIsSendAndWriteLogs(t *testing.T) {
	received := make(chan string, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- r.URL.Path + " " + string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	app := New(Config{SendTimeout: 2 * time.Second}, st, log.New(io.Discard, "", 0), filepath.Join(tempDir, "app.log"))
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	targetConfig, _ := json.Marshal(map[string]any{
		"server_url": backend.URL,
		"device_key": "target-test",
	})
	target, err := st.CreateTarget(context.Background(), model.Target{
		Name:    "target test",
		Type:    model.TargetTypeBark,
		Config:  string(targetConfig),
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	route, err := st.CreateRoute(context.Background(), model.Route{
		Key:       "route-test",
		Name:      "Route Test",
		Enabled:   true,
		TargetIDs: []int64{target.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	targetResp, err := http.Post(server.URL+"/api/targets/"+strconv.FormatInt(target.ID, 10)+"/test", "application/json", strings.NewReader(`{"title":"Target Title","message":"Target Message"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer targetResp.Body.Close()
	if targetResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(targetResp.Body)
		t.Fatalf("target test status=%d body=%s", targetResp.StatusCode, body)
	}
	if got := <-received; !strings.Contains(got, "Target Message") {
		t.Fatalf("target test did not send expected body: %s", got)
	}

	routeResp, err := http.Post(server.URL+"/api/routes/"+strconv.FormatInt(route.ID, 10)+"/test", "application/json", strings.NewReader(`{"title":"Route Title","message":"Route Message"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer routeResp.Body.Close()
	if routeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(routeResp.Body)
		t.Fatalf("route test status=%d body=%s", routeResp.StatusCode, body)
	}
	if got := <-received; !strings.Contains(got, "Route Message") {
		t.Fatalf("route test did not send expected body: %s", got)
	}

	logs, err := st.ListSendLogs(context.Background(), store.LogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2: %+v", len(logs), logs)
	}
}

func TestRuntimeLogsAPI(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "app.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	app := New(Config{}, st, log.New(io.Discard, "", 0), path)
	req := httptest.NewRequest(http.MethodGet, "/api/runtime-logs?limit=1", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "two") || strings.Contains(rec.Body.String(), "one") {
		t.Fatalf("unexpected runtime log response: %s", rec.Body.String())
	}
}

func TestWebPageIncludesUsageEntryAndRouteExamples(t *testing.T) {
	app := New(Config{}, nil, log.New(io.Discard, "", 0), "")
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`data-tab="help"`,
		"使用说明",
		"routeExamples(route)",
		"curl",
		"urllib.request",
		`<option value="board">board</option>`,
		`mode: "append"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("web page missing %q", want)
		}
	}

	cssResp, err := http.Get(server.URL + "/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer cssResp.Body.Close()
	css, err := io.ReadAll(cssResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".route-card") {
		t.Fatalf("css missing route card styles")
	}

	iconResp, err := http.Get(server.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	defer iconResp.Body.Close()
	if iconResp.StatusCode != http.StatusNoContent {
		t.Fatalf("favicon status=%d", iconResp.StatusCode)
	}
}
