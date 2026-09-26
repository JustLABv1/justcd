package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		"error", errors.New("renderer echoed password-canary"),
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
