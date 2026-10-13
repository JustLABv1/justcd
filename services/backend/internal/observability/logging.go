package observability

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"
)

// NewLogHandler retains redacted error diagnostics. Callers must still avoid
// logging raw response bodies or manifests as errors. Renderer stderr is
// accepted only via LogDiagnostic and redacted before reaching the handler.
func NewLogHandler(next slog.Handler) slog.Handler {
	return redactingHandler{next: next}
}

// StartupError carries initialization errors through the same redaction rules
// as ordinary errors. Retained for compatibility with existing startup callers.
type StartupError struct{ Err error }

var errorCredentials = regexp.MustCompile(`(?i)((?:["']?(?:password|passphrase|(?:access[_-]?|refresh[_-]?)?token|secret|authorization|cookie|credential|api[_-]?key|kubeconfig|known[_-]?hosts|client[_-]?certificate[_-]?data|certificate[_-]?authority[_-]?data|client[_-]?key[_-]?data)["']?)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
var errorURLCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)
var errorAuthToken = regexp.MustCompile(`(?i)((?:bearer|basic)\s+)[A-Za-z0-9._~+/-]+=*`)
var privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?(?:-----END [^-\r\n]*PRIVATE KEY-----|$)`)

func (e StartupError) message() string {
	if e.Err == nil {
		return ""
	}
	return redactErrorMessage(e.Err.Error())
}

func redactErrorMessage(message string) string {
	// Redact before truncating so a length limit cannot cut a credential or key
	// in half and leave its prefix in the log.
	message = privateKeyPattern.ReplaceAllString(message, "[redacted private key]")
	message = errorURLCredentials.ReplaceAllString(message, `${1}[redacted]@`)
	message = errorAuthToken.ReplaceAllString(message, `${1}[redacted]`)
	message = errorCredentials.ReplaceAllString(message, `${1}[redacted]`)
	const maxErrorMessageBytes = 4000
	if len(message) > maxErrorMessageBytes {
		end := maxErrorMessageBytes
		for !utf8.ValidString(message[:end]) && end > 0 {
			end--
		}
		message = message[:end] + "…"
	}
	return message
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
				message := err.Error()
				var diagnostic interface{ LogDiagnostic() string }
				if errors.As(err, &diagnostic) {
					if detail := diagnostic.LogDiagnostic(); detail != "" {
						message += ": " + detail
					}
				}
				return slog.String(attr.Key, redactErrorMessage(message))
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
