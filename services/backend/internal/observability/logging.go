package observability

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"
)

// NewLogHandler keeps structured errors useful without forwarding their text,
// which can contain user supplied values, credentials, or renderer output.
func NewLogHandler(next slog.Handler) slog.Handler {
	return redactingHandler{next: next}
}

// StartupError enables diagnostic text for trusted initialization errors only.
// Do not use it for renderer output or errors containing arbitrary user content.
type StartupError struct{ Err error }

var startupCredentials = regexp.MustCompile(`(?i)((?:["']?(?:password|passphrase|token|secret|authorization|cookie|credential|api[_-]?key)["']?)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
var startupURLCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)
var startupBearer = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`)

func (e StartupError) message() string {
	if e.Err == nil {
		return ""
	}
	message := startupURLCredentials.ReplaceAllString(e.Err.Error(), `${1}[redacted]@`)
	message = startupBearer.ReplaceAllString(message, `${1}[redacted]`)
	return startupCredentials.ReplaceAllString(message, `${1}[redacted]`)
}

type redactingHandler struct{ next slog.Handler }

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(redactAttr(attr))
		return true
	})
	if requestID := RequestID(ctx); requestID != "" {
		clean.AddAttrs(slog.String("request_id", requestID))
	}
	spanContext := trace.SpanFromContext(ctx).SpanContext()
	if spanContext.IsValid() {
		clean.AddAttrs(slog.String("trace_id", spanContext.TraceID().String()), slog.String("span_id", spanContext.SpanID().String()))
	}
	return h.next.Handle(ctx, clean)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		clean[i] = redactAttr(attr)
	}
	return redactingHandler{next: h.next.WithAttrs(clean)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	key := strings.ToLower(attr.Key)
	if key == "error" || strings.HasSuffix(key, "_error") {
		if attr.Value.Kind() == slog.KindAny {
			if diagnostic, ok := attr.Value.Any().(StartupError); ok {
				return slog.String(attr.Key, diagnostic.message())
			}
			if err, ok := attr.Value.Any().(error); ok {
				var databaseError *pgconn.PgError
				if errors.As(err, &databaseError) {
					// SQLSTATE identifies the failure without logging SQL,
					// user values, or PostgreSQL message/detail fields.
					return slog.Group(attr.Key, slog.String("type", reflect.TypeOf(err).String()), slog.String("sqlstate", databaseError.Code))
				}
				return slog.String(attr.Key, reflect.TypeOf(err).String())
			}
		}
		return slog.String(attr.Key, "redacted")
	}
	for _, sensitive := range []string{"password", "passphrase", "token", "secret", "credential", "authorization", "cookie", "privatekey", "private_key", "encryptionkey", "encryption_key", "cipher", "dsn", "url", "manifest", "payload", "header"} {
		if strings.Contains(key, sensitive) {
			return slog.String(attr.Key, "redacted")
		}
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		for i := range group {
			group[i] = redactAttr(group[i])
		}
		attr.Value = slog.GroupValue(group...)
	}
	return attr
}
