package notifications

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRoutingAndProviderFormatting(t *testing.T) {
	e := Event{SchemaVersion: 1, ID: "event-1", Type: "drift", Severity: "warning", WorkspaceID: "w", ApplicationID: "a", URL: "https://justcd.example/applications/a"}
	if !Routes([]string{"drift"}, "warning", e) || Routes([]string{"unhealthy"}, "info", e) || Routes([]string{"drift"}, "error", e) {
		t.Fatal("routing")
	}
	for _, provider := range []string{"slack", "teams", "webhook"} {
		body, err := Body(provider, Config{}, e)
		if err != nil || !json.Valid(body) || !strings.Contains(string(body), e.URL) {
			t.Fatalf("%s: %s %v", provider, body, err)
		}
	}
	body, err := Body("webhook", Config{Body: `{"title":{{type}},"description":{{url}},"event":{{event}}}`}, e)
	if err != nil || !json.Valid(body) {
		t.Fatalf("template: %s %v", body, err)
	}
	// Event carries no arbitrary resource payload, even when deserialized from unsafe input.
	var safe Event
	_ = json.Unmarshal([]byte(`{"id":"safe","credentials":{"password":"LEAK"},"data":{"secret":"LEAK"},"manifest":{"kind":"Secret","data":"LEAK"},"message":"LEAK"}`), &safe)
	raw, _ := json.Marshal(safe)
	if strings.Contains(string(raw), "LEAK") {
		t.Fatal("redaction allowlist")
	}
}
func TestDeliverySignaturesRetriesAndSafeErrors(t *testing.T) {
	for _, status := range []int{200, 400, 401, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Header.Get("Idempotency-Key") != "delivery-1" || r.Header.Get("X-JustCD-Event-ID") != "event-1" || r.Header.Get("X-JustCD-Signature") != Signature(strings.Repeat("s", 32), r.Header.Get("X-JustCD-Timestamp"), body) {
					t.Error("missing delivery identity or invalid signature")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("TOP-SECRET provider error"))
			}))
			defer server.Close()
			result := Deliver(context.Background(), server.Client(), "webhook", Config{URL: server.URL, SigningSecret: strings.Repeat("s", 32)}, Event{ID: "event-1"}, "delivery-1")
			if result.Retry != (status == 429 || status >= 500) || result.StatusCode != status || strings.Contains(result.Code, "TOP-SECRET") {
				t.Fatalf("%+v", result)
			}
		})
	}
	result := Deliver(context.Background(), http.DefaultClient, "webhook", Config{URL: "https://invalid.invalid", Body: "not-json"}, Event{}, "id")
	if result.Retry || result.Code != "invalid_template" {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = Deliver(ctx, http.DefaultClient, "webhook", Config{URL: "https://invalid.invalid"}, Event{}, "id")
	if !result.Retry || result.Code != "transport_failed" {
		t.Fatal(result)
	}
	if Backoff(1) != 30*time.Second || Backoff(5) != 8*time.Minute || Backoff(100) != 8*time.Minute {
		t.Fatal("unbounded backoff")
	}
}
func TestConfigurationAndNetworkProtection(t *testing.T) {
	for _, c := range []Config{{URL: "http://example.com", SigningSecret: strings.Repeat("s", 32)}, {URL: "https://user:secret@example.com", SigningSecret: strings.Repeat("s", 32)}, {URL: "https://example.com", SigningSecret: "short"}, {URL: "https://example.com", SigningSecret: strings.Repeat("s", 32), Headers: map[string]string{"Idempotency-Key": "fake"}}} {
		if c.Validate("webhook") == nil {
			t.Fatal("accepted unsafe config")
		}
	}
	if (Config{SMTPHost: "smtp.example.com", SMTPPort: 587, From: "a@example.com", To: []string{"b@example.com"}}).Validate("email") != nil {
		t.Fatal("valid email rejected")
	}
	if (Config{SMTPHost: "smtp.example.com", SMTPPort: 587, From: "a@example.com\r\nBcc:leak@example.com", To: []string{"b@example.com"}}).Validate("email") == nil {
		t.Fatal("header injection")
	}
	client := PublicClient()
	defer client.CloseIdleConnections()
	r := Deliver(context.Background(), client, "webhook", Config{URL: "https://127.0.0.1", SigningSecret: strings.Repeat("s", 32)}, Event{}, "id")
	if r.Code != "transport_failed" {
		t.Fatal("loopback not blocked")
	}
	if smtpFailure(&textproto.Error{Code: 535, Msg: "secret-password"}).Retry || !smtpFailure(&textproto.Error{Code: 450, Msg: "temporary"}).Retry {
		t.Fatal("SMTP failure classification")
	}
}

func TestSMTPRequiresTLSAndHidesProviderErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "220 test SMTP\r\n")
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "EHLO") {
				fmt.Fprint(conn, "250 test\r\n")
			} else if strings.HasPrefix(line, "STARTTLS") {
				fmt.Fprint(conn, "454 TOP-SECRET provider error\r\n")
				return
			} else {
				fmt.Fprint(conn, "500 rejected\r\n")
				return
			}
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	result := Deliver(context.Background(), nil, "email", Config{SMTPHost: "127.0.0.1", SMTPPort: number, From: "a@example.com", To: []string{"b@example.com"}}, Event{SchemaVersion: 1}, "id")
	if result.Retry || result.Code != "smtp_tls_failed" || strings.Contains(result.Code, "TOP-SECRET") {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = Deliver(ctx, nil, "email", Config{SMTPHost: "127.0.0.1", SMTPPort: number}, Event{}, "id")
	if !result.Retry || result.Code != "smtp_transport_failed" {
		t.Fatal(result)
	}
}
