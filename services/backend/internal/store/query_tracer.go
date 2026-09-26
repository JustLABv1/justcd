package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer records database operation spans without attaching SQL text,
// bound parameters, connection details, or error messages.
type QueryTracer struct{ tracer trace.Tracer }

type querySpanContextKey struct{}

func NewQueryTracer(provider trace.TracerProvider) *QueryTracer {
	return &QueryTracer{tracer: provider.Tracer("justcd/store")}
}

func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	operation := queryOperation(data.SQL)
	ctx, span := t.tracer.Start(ctx, "postgres "+operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.operation.name", operation),
	))
	return context.WithValue(ctx, querySpanContextKey{}, span)
}

func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(querySpanContextKey{}).(trace.Span)
	if !ok {
		return
	}
	if data.Err != nil {
		span.SetAttributes(attribute.String("error.type", "database_error"))
		span.SetStatus(codes.Error, "")
	}
	span.End()
}

func queryOperation(sql string) string {
	statement := strings.TrimSpace(sql)
	for strings.HasPrefix(statement, "--") {
		_, remainder, found := strings.Cut(statement, "\n")
		if !found {
			return "OTHER"
		}
		statement = strings.TrimSpace(remainder)
	}
	if strings.HasPrefix(statement, "/*") {
		_, remainder, found := strings.Cut(statement, "*/")
		if !found {
			return "OTHER"
		}
		statement = strings.TrimSpace(remainder)
	}
	word, _, _ := strings.Cut(statement, " ")
	if word == "" {
		word, _, _ = strings.Cut(statement, "\n")
	}
	operation := strings.ToUpper(strings.Trim(word, "\t\r\n;"))
	switch operation {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "BEGIN", "COMMIT", "ROLLBACK", "CREATE", "ALTER", "DROP", "TRUNCATE", "SAVEPOINT", "RELEASE":
		return operation
	default:
		return "OTHER"
	}
}
