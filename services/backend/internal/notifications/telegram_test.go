package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTelegramFormattingResponseAndRedaction(t *testing.T) {
	c := Config{BotToken: "123456:abcdefghijklmnopqrstuvwxyz123456789", ChatID: "-1234", MessageThreadID: 42}
	if err := c.Validate("telegram"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body  string
		retry bool
		code  string
	}{{`{"ok":true}`, false, "delivered"}, {`{"ok":false,"error_code":429,"description":"TOP-SECRET"}`, true, "telegram_rejected"}, {`{"ok":false,"error_code":403,"description":"TOP-SECRET"}`, false, "telegram_rejected"}, {`not-json`, true, "telegram_invalid_response"}} {
		client := &http.Client{Transport: telegramTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot"+c.BotToken+"/sendMessage" {
				t.Fatal("wrong Telegram endpoint")
			}
			var data map[string]any
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Fatal(err)
			}
			if data["chat_id"] != "-1234" || data["message_thread_id"] != float64(42) || !strings.Contains(data["text"].(string), "https://justcd.example/applications/prod") || !strings.Contains(data["text"].(string), "Schema v1") {
				t.Fatal("Telegram formatting", data)
			}
			if r.Header.Get("Idempotency-Key") != "delivery" {
				t.Fatal("missing delivery identity")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
		})}
		result := Deliver(context.Background(), client, "telegram", c, Event{SchemaVersion: 1, ID: "event", Type: "sync_failed", Severity: "error", URL: "https://justcd.example/applications/prod"}, "delivery")
		if result.Code != tc.code || result.Retry != tc.retry || strings.Contains(result.Code, "TOP-SECRET") {
			t.Fatal(result)
		}
	}
	for _, bad := range []Config{{BotToken: "short", ChatID: "123"}, {BotToken: c.BotToken + "/../leak", ChatID: "123"}, {BotToken: c.BotToken, ChatID: "bad chat"}, {BotToken: c.BotToken, ChatID: "123", MessageThreadID: -1}} {
		if bad.Validate("telegram") == nil {
			t.Fatal("unsafe Telegram config accepted")
		}
	}
}
