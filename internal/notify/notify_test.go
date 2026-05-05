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
