// Package notifications formats allowlisted events and delivers them outside reconciliation.
package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Event struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Severity      string    `json:"severity"`
	WorkspaceID   string    `json:"workspaceId"`
	ApplicationID string    `json:"applicationId,omitempty"`
	ResourceID    string    `json:"resourceId,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
	URL           string    `json:"url"`
}

var EventTypes = []string{"drift", "approval_requested", "sync_failed", "unhealthy", "operation_completed", "rollback", "test"}

func Severity(s string) int {
	switch s {
	case "info":
		return 1
	case "warning":
		return 2
	case "error":
		return 3
	}
	return 0
}
func Routes(events []string, minimum string, e Event) bool {
	if Severity(e.Severity) < Severity(minimum) {
		return false
	}
	for _, t := range events {
		if t == e.Type {
			return true
		}
	}
	return false
}

// Config is encrypted as a single value. Never return it in destination or delivery responses.
type Config struct {
	BotToken        string            `json:"botToken,omitempty"`
	ChatID          string            `json:"chatId,omitempty"`
	MessageThreadID int               `json:"messageThreadId,omitempty"`
	URL             string            `json:"url,omitempty"`
	Method          string            `json:"method,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	SigningSecret   string            `json:"signingSecret,omitempty"`
	SMTPHost        string            `json:"smtpHost,omitempty"`
	SMTPPort        int               `json:"smtpPort,omitempty"`
	Username        string            `json:"username,omitempty"`
	Password        string            `json:"password,omitempty"`
	From            string            `json:"from,omitempty"`
	To              []string          `json:"to,omitempty"`
}

func (c Config) Validate(provider string) error {
	switch provider {
	case "telegram":
		if !regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{20,200}$`).MatchString(c.BotToken) {
			return errors.New("a valid Telegram bot token is required")
		}
		if len(c.ChatID) > 128 || !regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z0-9_]+)$`).MatchString(c.ChatID) || c.MessageThreadID < 0 {
			return errors.New("a numeric chat ID or @channel name and optional positive topic ID are required")
		}
	case "email":
		if c.SMTPHost == "" || strings.ContainsAny(c.SMTPHost, "\r\n/: ") || c.SMTPPort < 1 || c.SMTPPort > 65535 || len(c.To) == 0 || len(c.To) > 30 {
			return errors.New("SMTP host, port, from and recipients are required")
		}
		for _, a := range append(append([]string{}, c.To...), c.From) {
			if strings.ContainsAny(a, "\r\n") {
				return errors.New("invalid email address")
			}
			if _, err := mail.ParseAddress(a); err != nil {
				return errors.New("invalid email address")
			}
		}
	case "webhook", "slack", "teams":
		u, err := url.Parse(c.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return errors.New("an HTTPS URL without embedded credentials is required")
		}
		method := c.Method
		if method == "" {
			method = "POST"
		}
		if method != "POST" && method != "PUT" && method != "PATCH" {
			return errors.New("method must be POST, PUT or PATCH")
		}
		if len(c.Body) > 65536 || len(c.Headers) > 30 {
			return errors.New("webhook configuration too large")
		}
		for k, v := range c.Headers {
			if !validHeader(k) || strings.ContainsAny(v, "\r\n") {
				return errors.New("invalid header")
			}
			switch strings.ToLower(k) {
			case "host", "content-length", "connection", "transfer-encoding", "idempotency-key", "x-justcd-signature", "x-justcd-event-id", "x-justcd-timestamp":
				return errors.New("reserved header")
			}
		}
		if len(c.SigningSecret) < 32 {
			return errors.New("signing secret must contain at least 32 characters")
		}
	default:
		return errors.New("unsupported provider")
	}
	return nil
}
func validHeader(k string) bool {
	if k == "" {
		return false
	}
	for _, b := range []byte(k) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b))) {
			return false
		}
	}
	return true
}
func Body(provider string, c Config, e Event) ([]byte, error) {
	message := fmt.Sprintf("JustCD %s (%s)\n%s", e.Type, e.Severity, e.URL)
	switch provider {
	case "telegram":
		data := map[string]any{"chat_id": c.ChatID, "text": fmt.Sprintf("%s\nSchema v%d · Event %s\nWorkspace %s · Application %s\n%s", message, e.SchemaVersion, e.ID, e.WorkspaceID, e.ApplicationID, e.OccurredAt.UTC().Format(time.RFC3339)), "link_preview_options": map[string]bool{"is_disabled": true}}
		if c.MessageThreadID > 0 {
			data["message_thread_id"] = c.MessageThreadID
		}
		return json.Marshal(data)
	case "slack":
		return json.Marshal(map[string]any{"text": message, "metadata": map[string]any{"event_type": "justcd_v1", "event_payload": e}})
	case "teams":
		facts := []any{}
		for _, fact := range [][2]string{{"schemaVersion", strconv.Itoa(e.SchemaVersion)}, {"id", e.ID}, {"type", e.Type}, {"severity", e.Severity}, {"workspaceId", e.WorkspaceID}, {"applicationId", e.ApplicationID}, {"resourceId", e.ResourceID}, {"occurredAt", e.OccurredAt.Format(time.RFC3339)}} {
			facts = append(facts, map[string]string{"title": fact[0], "value": fact[1]})
		}
		return json.Marshal(map[string]any{"type": "message", "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "content": map[string]any{"type": "AdaptiveCard", "version": "1.2", "body": []any{map[string]any{"type": "TextBlock", "text": message, "wrap": true}, map[string]any{"type": "FactSet", "facts": facts}}, "actions": []any{map[string]any{"type": "Action.OpenUrl", "title": "Open JustCD", "url": e.URL}}}}}})
	}
	if c.Body != "" {
		raw, _ := json.Marshal(e)
		// {{event}} is raw JSON; scalar placeholders are JSON-escaped including quotes.
		replacements := []string{"{{event}}", string(raw)}
		for k, v := range map[string]string{"id": e.ID, "type": e.Type, "severity": e.Severity, "url": e.URL, "workspaceId": e.WorkspaceID, "applicationId": e.ApplicationID, "resourceId": e.ResourceID} {
			b, _ := json.Marshal(v)
			replacements = append(replacements, "{{"+k+"}}", string(b))
		}
		body := strings.NewReplacer(replacements...).Replace(c.Body)
		if !json.Valid([]byte(body)) {
			return nil, errors.New("invalid JSON body template")
		}
		return []byte(body), nil
	}
	return json.Marshal(e)
}
func Signature(secret, timestamp string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(timestamp + "."))
	h.Write(body)
	return "v1=" + hex.EncodeToString(h.Sum(nil))
}

// PublicClient prevents redirects, DNS rebinding and access to private/link-local services.
func PublicClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no address")
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, errors.New("non-public destination")
			}
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}}
}

// Result contains only safe diagnostics. Remote response bodies and errors may contain credentials.
type Result struct {
	StatusCode int
	Retry      bool
	Code       string
}

func Deliver(ctx context.Context, client *http.Client, provider string, c Config, e Event, id string) Result {
	if provider == "email" {
		return deliverEmail(ctx, c, e, id)
	}
	body, err := Body(provider, c, e)
	if err != nil {
		return Result{Code: "invalid_template"}
	}
	method := c.Method
	if method == "" || provider != "webhook" {
		method = "POST"
	}
	endpoint := c.URL
	signingKey := c.SigningSecret
	if provider == "telegram" {
		endpoint = "https://api.telegram.org/bot" + c.BotToken + "/sendMessage"
		signingKey = c.BotToken
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{Code: "invalid_destination"}
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", id)
	req.Header.Set("X-JustCD-Event-ID", e.ID)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-JustCD-Timestamp", timestamp)
	req.Header.Set("X-JustCD-Signature", Signature(signingKey, timestamp, body))
	res, err := client.Do(req)
	if err != nil {
		return Result{Retry: true, Code: "transport_failed"}
	}
	defer res.Body.Close()
	if provider == "telegram" && res.StatusCode >= 200 && res.StatusCode < 300 {
		var response struct {
			OK        bool `json:"ok"`
			ErrorCode int  `json:"error_code"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&response) != nil {
			return Result{StatusCode: res.StatusCode, Code: "telegram_invalid_response", Retry: true}
		}
		if !response.OK {
			return Result{StatusCode: response.ErrorCode, Code: "telegram_rejected", Retry: response.ErrorCode == 429 || response.ErrorCode >= 500}
		}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return Result{StatusCode: res.StatusCode, Code: "delivered"}
	}
	return Result{StatusCode: res.StatusCode, Retry: res.StatusCode == 408 || res.StatusCode == 429 || res.StatusCode >= 500, Code: "provider_rejected"}
}
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(1<<uint(attempt-1)) * 30 * time.Second
}
func deliverEmail(ctx context.Context, c Config, e Event, id string) Result {
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort)))
	if err != nil {
		return Result{Code: "smtp_transport_failed", Retry: true}
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		return smtpFailure(err)
	}
	defer client.Close()
	if err = client.StartTLS(&tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
		return Result{Code: "smtp_tls_failed"}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.SMTPHost)); err != nil {
			return smtpFailure(err)
		}
	}
	from, _ := mail.ParseAddress(c.From)
	if err = client.Mail(from.Address); err != nil {
		return smtpFailure(err)
	}
	for _, to := range c.To {
		a, _ := mail.ParseAddress(to)
		if err = client.Rcpt(a.Address); err != nil {
			return smtpFailure(err)
		}
	}
	out, err := client.Data()
	if err != nil {
		return smtpFailure(err)
	}
	_, err = fmt.Fprintf(out, "From: %s\r\nTo: %s\r\nSubject: JustCD %s (%s)\r\nMessage-ID: <%s@justcd>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nEvent: %s\r\nSchema: v%d\r\nEvent ID: %s\r\nWorkspace: %s\r\nApplication: %s\r\nOccurred at: %s\r\n%s\r\n", c.From, strings.Join(c.To, ", "), e.Type, e.Severity, id, e.Type, e.SchemaVersion, e.ID, e.WorkspaceID, e.ApplicationID, e.OccurredAt.Format(time.RFC3339), e.URL)
	if err != nil {
		return smtpFailure(err)
	}
	if err = out.Close(); err != nil {
		return smtpFailure(err)
	}
	_ = client.Quit()
	return Result{Code: "delivered"}
}
func smtpFailure(err error) Result { // SMTP 4xx and network errors are transient; 5xx failures are terminal.
	retry := true
	var e *textproto.Error
	if errors.As(err, &e) && e.Code >= 500 {
		retry = false
	}
	return Result{Code: "smtp_failed", Retry: retry}
}
