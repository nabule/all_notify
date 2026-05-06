package notify

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"all_notify/internal/model"
)

func TestDispatcherSendsBarkAndNtfyHTTP(t *testing.T) {
	seen := make(chan string, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- fmt.Sprintf("%s %s %s %s", r.Method, r.URL.Path, r.Header.Get("Title"), string(body))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	barkConfig, _ := json.Marshal(BarkConfig{ServerURL: backend.URL, DeviceKey: "abc"})
	ntfyConfig, _ := json.Marshal(NtfyConfig{ServerURL: backend.URL, Topic: "topic", Tags: []string{"tag1"}})
	targets := []model.Target{
		{ID: 1, Name: "bark", Type: model.TargetTypeBark, Config: string(barkConfig), Enabled: true},
		{ID: 2, Name: "ntfy", Type: model.TargetTypeNtfy, Config: string(ntfyConfig), Enabled: true},
	}
	dispatcher := NewDispatcher(2 * time.Second)
	results := dispatcher.SendAll(context.Background(), model.Notification{Title: "Hello", Message: "World"}, targets)
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	for _, result := range results {
		if result.Status != model.StatusSuccess {
			t.Fatalf("unexpected result: %+v", result)
		}
	}

	gotA := <-seen
	gotB := <-seen
	combined := gotA + "\n" + gotB
	if !strings.Contains(combined, "POST /abc") || !strings.Contains(combined, `"body":"World"`) {
		t.Fatalf("Bark request not observed: %s", combined)
	}
	if !strings.Contains(combined, "POST /topic Hello World") {
		t.Fatalf("ntfy request not observed: %s", combined)
	}
}

func TestDispatcherSendsBoardAppendAndOverwriteHTTP(t *testing.T) {
	type observedRequest struct {
		path          string
		authorization string
		action        string
		content       string
	}
	seen := make(chan observedRequest, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Action  string `json:"action"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		seen <- observedRequest{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			action:        payload.Action,
			content:       payload.Content,
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	dispatcher := NewDispatcher(2 * time.Second)
	for _, tc := range []struct {
		name string
		mode string
		want string
	}{
		{name: "append", mode: "append", want: "append"},
		{name: "overwrite", mode: "new", want: "new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boardConfig, _ := json.Marshal(BoardConfig{
				ServerURL: backend.URL,
				BoardID:   "hr",
				APIToken:  "admin123",
				Mode:      tc.mode,
			})
			target := model.Target{ID: 3, Name: "board", Type: model.TargetTypeBoard, Config: string(boardConfig), Enabled: true}
			results := dispatcher.SendAll(context.Background(), model.Notification{
				Title:   "Ignored",
				Message: "公告内容",
				URL:     "https://example.com/detail",
			}, []model.Target{target})
			if len(results) != 1 || results[0].Status != model.StatusSuccess {
				t.Fatalf("unexpected result: %+v", results)
			}

			got := <-seen
			if got.path != "/api/update/hr" {
				t.Fatalf("unexpected path: %s", got.path)
			}
			if got.authorization != "Bearer admin123" {
				t.Fatalf("unexpected auth header: %s", got.authorization)
			}
			if got.action != tc.want {
				t.Fatalf("unexpected action: %s", got.action)
			}
			if got.content != "公告内容\n\nhttps://example.com/detail" {
				t.Fatalf("unexpected content: %q", got.content)
			}
		})
	}
}

func TestValidateBoardTargetMode(t *testing.T) {
	validConfig, _ := json.Marshal(BoardConfig{
		ServerURL: "https://board.12342345.xyz",
		BoardID:   "hr",
		APIToken:  "admin123",
		Mode:      "new",
	})
	if err := ValidateTarget(model.Target{Name: "board", Type: model.TargetTypeBoard, Config: string(validConfig), Enabled: true}); err != nil {
		t.Fatalf("valid board target rejected: %v", err)
	}

	invalidConfig, _ := json.Marshal(BoardConfig{
		ServerURL: "https://board.12342345.xyz",
		BoardID:   "hr",
		APIToken:  "admin123",
		Mode:      "clear",
	})
	if err := ValidateTarget(model.Target{Name: "board", Type: model.TargetTypeBoard, Config: string(invalidConfig), Enabled: true}); err == nil {
		t.Fatal("invalid board mode accepted")
	}
}

func TestSMTPSendWithFakeServer(t *testing.T) {
	smtpServer, messages := startFakeSMTP(t)
	defer smtpServer.Close()
	host, portText, err := net.SplitHostPort(smtpServer.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	config, _ := json.Marshal(SMTPConfig{
		Host:          host,
		Port:          port,
		Security:      "none",
		From:          "sender@example.com",
		To:            []string{"receiver@example.com"},
		SubjectPrefix: "[test]",
	})
	response, err := sendSMTP(context.Background(), model.Notification{Title: "Title", Message: "Body"}, string(config), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response != "smtp sent" {
		t.Fatalf("unexpected response: %s", response)
	}
	message := <-messages
	if !strings.Contains(message, "Subject: [test] Title") {
		t.Fatalf("missing subject in message: %s", message)
	}
	if !strings.Contains(message, base64.StdEncoding.EncodeToString([]byte("Body"))) {
		t.Fatalf("missing encoded body in message: %s", message)
	}
}

type fakeSMTP struct {
	listener net.Listener
}

func (s *fakeSMTP) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *fakeSMTP) Close() error {
	return s.listener.Close()
}

func startFakeSMTP(t *testing.T) (*fakeSMTP, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		writeLine := func(line string) {
			_, _ = conn.Write([]byte(line + "\r\n"))
		}
		writeLine("220 fake smtp")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				writeLine("250-localhost")
				writeLine("250 OK")
			case strings.HasPrefix(cmd, "MAIL FROM:"), strings.HasPrefix(cmd, "RCPT TO:"):
				writeLine("250 OK")
			case cmd == "DATA":
				writeLine("354 End data with <CR><LF>.<CR><LF>")
				var b strings.Builder
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimSpace(dataLine) == "." {
						break
					}
					b.WriteString(dataLine)
				}
				messages <- b.String()
				writeLine("250 OK")
			case cmd == "QUIT":
				writeLine("221 Bye")
				return
			default:
				writeLine("250 OK")
			}
		}
	}()
	return &fakeSMTP{listener: ln}, messages
}
