package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Telemetry struct {
	Metrics *Metrics
	traces  *sdktrace.TracerProvider
}

func Init(ctx context.Context, cfg config.Observability) (*Telemetry, error) {
	t := &Telemetry{Metrics: NewMetrics(cfg.MetricsEnabled)}
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if !cfg.TracingEnabled {
		return t, nil
	}
	if strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		return nil, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT is required when tracing is enabled")
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, errors.New("initialize OTLP trace exporter")
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio))),
		sdktrace.WithResource(sdkresource.NewSchemaless(attribute.String("service.name", serviceName()))),
	)
	otel.SetTracerProvider(provider)
	t.traces = provider
	return t, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || t.traces == nil {
		return nil
	}
	return t.traces.Shutdown(ctx)
}

func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func serviceName() string {
	if value := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); value != "" {
		return value
	}
	return "justcd-backend"
}

func ContextFromTraceParent(ctx context.Context, traceParent string) context.Context {
	if strings.TrimSpace(traceParent) == "" {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": traceParent})
}

func MarkError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.SetAttributes(attribute.String("error.type", reflect.TypeOf(err).String()))
	span.SetStatus(codes.Error, "")
}

func HTTPMiddleware(next http.Handler, logger *slog.Logger, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		route := RouteLabel(r.URL.Path)
		parent := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("justcd/http").Start(parent, "HTTP "+boundedMethod(r.Method)+" "+route, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		requestID := RequestIDFromTrace(span.SpanContext())
		ctx = context.WithValue(ctx, requestIDKey{}, requestID)
		w.Header().Set("X-Request-ID", requestID)
		span.SetAttributes(attribute.String("http.request.method", boundedMethod(r.Method)), attribute.String("http.route", route))
		status := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(status, r.WithContext(ctx))
		elapsed := time.Since(started)
		span.SetAttributes(attribute.Int("http.response.status_code", status.status))
		if status.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "")
		}
		if metrics != nil {
			metrics.RecordHTTP(r.Method, route, status.status, elapsed)
		}
		if logger != nil {
			logger.InfoContext(ctx, "HTTP request completed", "method", boundedMethod(r.Method), "route", route, "status", status.status, "duration_ms", elapsed.Milliseconds())
		}
	})
}

func RouteLabel(path string) string {
	switch {
	case path == "/healthz":
		return "/healthz"
	case path == "/api/v1/health":
		return "/api/v1/health"
	case path == "/api/v1/openapi.yaml":
		return "/api/v1/openapi.yaml"
	case path == "/metrics":
		return "/metrics"
	case strings.HasPrefix(path, "/api/v1/auth/"):
		return "/api/v1/auth/*"
	case strings.HasPrefix(path, "/api/v1/webhooks/"):
		return "/api/v1/webhooks/*"
	case strings.HasPrefix(path, "/api/v1/"):
		return "/api/v1/*"
	default:
		return "other"
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func RequestIDFromTrace(spanContext trace.SpanContext) string {
	if spanContext.IsValid() {
		return spanContext.TraceID().String()
	}
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(value[:])
}
