package syncer

import (
	"context"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type testRetryError struct {
	retryable bool
	code      string
}

func (e testRetryError) Error() string     { return "dependency failed" }
func (e testRetryError) Retryable() bool   { return e.retryable }
func (e testRetryError) ErrorCode() string { return e.code }

func retryTestPolicy() store.RetryPolicy {
	return store.RetryPolicy{Enabled: true, MaxAttempts: 5, InitialDelaySeconds: 5, MaxDelaySeconds: 20, JitterPercent: 20}
}

func TestComputeRetryDelayExponentialBackoffAndJitter(t *testing.T) {
	policy := retryTestPolicy()
	tests := []struct {
		name    string
		attempt int
		sample  float64
		want    time.Duration
	}{
		{name: "minimum jitter", attempt: 1, sample: 0, want: 4 * time.Second},
		{name: "second attempt", attempt: 2, sample: 0.5, want: 10 * time.Second},
		{name: "jitter clamped at maximum", attempt: 4, sample: 1, want: 20 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ComputeRetryDelay(policy, test.attempt, test.sample); got != test.want {
				t.Fatalf("ComputeRetryDelay() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestClassifyRetryError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
		code      string
	}{
		{name: "network", err: errors.New("connection refused"), retryable: true, code: "dependency.connection_failed"},
		{name: "kubernetes timeout", err: apierrors.NewTimeoutError("temporary timeout", 1), retryable: true, code: "kubernetes.transient_failure"},
		{name: "database serialization", err: &pgconn.PgError{Code: "40001"}, retryable: true, code: "database.transient_failure"},
		{name: "typed Git fetch timeout", err: testRetryError{retryable: true, code: "git.fetch_failed"}, retryable: true, code: "git.fetch_failed"},
		{name: "typed render validation", err: testRetryError{retryable: false, code: "render.command_failed"}, retryable: false, code: "render.command_failed"},
		{name: "authorization is terminal", err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "api", errors.New("denied")), retryable: false, code: "kubernetes.authorization_failed"},
		{name: "validation is terminal", err: apierrors.NewBadRequest("invalid manifest"), retryable: false, code: "kubernetes.validation_failed"},
		{name: "ordinary validation", err: errors.New("invalid manifest"), retryable: false, code: "operation.validation_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyRetryError(test.err)
			if got.Retryable != test.retryable || got.ErrorCode != test.code {
				t.Fatalf("ClassifyRetryError() = %#v, want retryable=%t code=%q", got, test.retryable, test.code)
			}
		})
	}
}

func TestDecideRetryEnforcesPolicyAndCancellation(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	ctx := context.Background()
	transient := errors.New("connection reset")

	scheduled := DecideRetry(ctx, retryTestPolicy(), 2, transient, now, 0.5, true)
	if scheduled.NextRetryAt == nil || !scheduled.NextRetryAt.Equal(now.Add(10*time.Second)) || scheduled.TerminalReason != "" {
		t.Fatalf("expected second transient attempt to schedule a retry, got %#v", scheduled)
	}

	for _, test := range []struct {
		name    string
		policy  store.RetryPolicy
		ctx     context.Context
		attempt int
		auto    bool
		want    string
	}{
		{name: "max attempts", policy: retryTestPolicy(), ctx: ctx, attempt: 5, auto: true, want: "max_attempts_exceeded"},
		{name: "disabled", policy: store.RetryPolicy{Enabled: false, MaxAttempts: 5, InitialDelaySeconds: 5, MaxDelaySeconds: 20}, ctx: ctx, attempt: 1, auto: true, want: "retry_disabled"},
		{name: "manual operation", policy: retryTestPolicy(), ctx: ctx, attempt: 1, auto: false, want: "manual_retry_required"},
		{name: "cancelled context", policy: retryTestPolicy(), ctx: cancelledContext(t), attempt: 1, auto: true, want: "operation_cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := DecideRetry(test.ctx, test.policy, test.attempt, transient, now, 0.5, test.auto)
			if got.NextRetryAt != nil || got.TerminalReason != test.want {
				t.Fatalf("DecideRetry() = %#v, want terminal reason %q", got, test.want)
			}
		})
	}
}

func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestAgentUnknownWriteIsNotRetriedThroughHTTPWrapper(t *testing.T) {
	err := &url.Error{Op: "Patch", URL: "https://justcd-agent.invalid", Err: &kube.AgentError{Code: "cluster.agent_unknown_outcome", Message: "execution outcome unknown"}}
	got := ClassifyRetryError(err)
	if got.Retryable || got.ErrorCode != "cluster.agent_unknown_outcome" {
		t.Fatalf("unsafe retry classification: %+v", got)
	}
}
