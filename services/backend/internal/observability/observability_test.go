package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLogHandlerRedactsSecretsAndAddsCorrelationIDs(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewLogHandler(slog.NewJSONHandler(&output, nil)))
	ctx := context.WithValue(context.Background(), requestIDKey{}, "request-canary")
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	ctx, span := provider.Tracer("test").Start(ctx, "request")
	logger.ErrorContext(ctx, "render failed",
		"error", errors.New("render failed: password=password-canary"),
		"apiToken", "token-canary",
		"details", slog.Group("headers", "Authorization", "bearer-canary"),
	)
	spanContext := span.SpanContext()
	span.End()

	line := output.String()
	for _, secret := range []string{"password-canary", "token-canary", "bearer-canary"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log included sensitive value %q: %s", secret, line)
		}
	}
	for _, correlation := range []string{"request-canary", spanContext.TraceID().String(), spanContext.SpanID().String()} {
		if !strings.Contains(line, correlation) {
			t.Fatalf("log omitted correlation value %q: %s", correlation, line)
		}
	}
}

func TestLogHandlerRetainsWrappedErrorDiagnostics(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(NewLogHandler(slog.NewJSONHandler(&output, nil)))
			err := fmt.Errorf("render Helm chart for namespace %q: %w", "production", errors.New("manifestPath cannot be resolved: no such file or directory"))
			attr := slog.Group("plan", slog.Any("error", err), slog.Any("previous_error", err))
			if bound {
				logger.With(attr).Warn("plan calculation failed")
			} else {
				logger.Warn("plan calculation failed", attr)
			}
			var entry struct {
				Plan map[string]string `json:"plan"`
			}
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"error", "previous_error"} {
				if entry.Plan[key] != err.Error() {
					t.Fatalf("missing wrapped diagnostic: %s", output.String())
				}
			}
		})
	}
}

func TestErrorDiagnosticsRedactCredentials(t *testing.T) {
	for _, message := range []string{
		`fetch https://user:url-canary@example.com/chart failed: access_token=access-canary&refreshToken=refresh-canary; api_key="api canary"; password='password canary'; Authorization: Bearer bearer-canary`,
		"invalid client-key-data: key-canary; kubeconfig=config-canary; cookie=cookie-canary",
		"HTTP request failed: Authorization: Basic basic-canary",
		"load key: -----BEGIN RSA PRIVATE KEY-----\nkey-canary\n-----END RSA PRIVATE KEY-----: invalid key",
		"load key: -----BEGIN PRIVATE KEY-----\nkey-canary",
	} {
		var output bytes.Buffer
		logger := slog.New(NewLogHandler(slog.NewJSONHandler(&output, nil)))
		logger.Error("failed", "error", errors.New(message))
		for _, secret := range []string{"url-canary", "access-canary", "refresh-canary", "api canary", "password canary", "bearer-canary", "basic-canary", "key-canary", "config-canary", "cookie-canary"} {
			if strings.Contains(output.String(), secret) {
				t.Fatalf("leaked %q: %s", secret, output.String())
			}
		}
		if !strings.Contains(output.String(), "[redacted") {
			t.Fatalf("missing redaction marker: %s", output.String())
		}
	}
}

func TestErrorDiagnosticsTruncateAfterRedaction(t *testing.T) {
	message := strings.Repeat("x", 3999) + "€" + strings.Repeat("y", 100) + " password=" + strings.Repeat("secret-canary", 500)
	got := redactErrorMessage(message)
	if !utf8.ValidString(got) || len(got) > 4003 || !strings.HasSuffix(got, "…") || strings.Contains(got, "secret-canary") {
		t.Fatalf("invalid truncated diagnostic: %q", got)
	}
	got = redactErrorMessage("password=" + strings.Repeat("secret-canary", 500))
	if got != "password=[redacted]" {
		t.Fatalf("credential was truncated before redaction: %q", got)
	}
}

func TestHTTPMetricsNormalizeUntrustedLabels(t *testing.T) {
	metrics := NewMetrics(true)
	metrics.RecordHTTP("BREW", "/api/v1/applications/application-canary", httpStatusServerError, time.Second)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))

	body := response.Body.String()
	if !strings.Contains(body, `method="OTHER",route="other",status="5xx"`) {
		t.Fatalf("metrics did not normalize label values: %s", body)
	}
	if strings.Contains(body, "application-canary") {
		t.Fatalf("metrics included a raw application identifier: %s", body)
	}
}

const httpStatusServerError = 503

func TestStartupErrorDiagnostics(t *testing.T) {
	for _, bound := range []bool{false, true} {
		var output bytes.Buffer
		logger := slog.New(NewLogHandler(slog.NewJSONHandler(&output, nil)))
		err := fmt.Errorf("initialize database: %w", errors.New("postgres://admin:url-canary@db:5432/app?password=query-canary connection refused; password='space canary'; Authorization=Bearer bearer-canary"))
		attr := slog.Group("startup", slog.Any("error", StartupError{Err: err}))
		if bound {
			logger.With(attr).Error("JustCD stopped")
		} else {
			logger.Error("JustCD stopped", attr)
		}
		line := output.String()
		for _, secret := range []string{"url-canary", "query-canary", "space canary", "bearer-canary"} {
			if strings.Contains(line, secret) {
				t.Fatalf("leaked %s: %s", secret, line)
			}
		}
		for _, diagnostic := range []string{"initialize database:", "connection refused"} {
			if !strings.Contains(line, diagnostic) {
				t.Fatalf("missing diagnostic: %s", line)
			}
		}
	}
}

func TestLogHandlerRetainsSQLStateWithoutDatabaseDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewLogHandler(slog.NewJSONHandler(&output, nil)))
	logger.Error("OIDC user mapping failed", "error", fmt.Errorf("audit: %w", &pgconn.PgError{Code: "42P18", Message: "password-canary", Detail: "token-canary", InternalQuery: "query-canary"}))
	line := output.String()
	if !strings.Contains(line, `"sqlstate":"42P18"`) {
		t.Fatalf("missing SQLSTATE: %s", line)
	}
	for _, secret := range []string{"password-canary", "token-canary", "query-canary"} {
		if strings.Contains(line, secret) {
			t.Fatalf("leaked database details: %s", line)
		}
	}
}
