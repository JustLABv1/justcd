package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestQueryTracerOmitsSQLParametersAndErrorMessages(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	tracer := NewQueryTracer(provider)

	ctx, parent := provider.Tracer("test").Start(context.Background(), "request")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL:  "INSERT INTO credentials(secret_cipher) VALUES($1)",
		Args: []any{"query-credential-canary"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("database echoed rendered-secret-canary")})
	parent.End()

	serialized, err := json.Marshal(exporter.GetSpans())
	if err != nil {
		t.Fatal(err)
	}
	traceOutput := string(serialized)
	for _, secret := range []string{"secret_cipher", "query-credential-canary", "rendered-secret-canary"} {
		if strings.Contains(traceOutput, secret) {
			t.Fatalf("trace included sensitive value %q: %s", secret, traceOutput)
		}
	}
	if !strings.Contains(traceOutput, "database_error") {
		t.Fatalf("trace did not preserve a safe error classification: %s", traceOutput)
	}
}
