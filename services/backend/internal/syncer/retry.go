package syncer

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type RetryClassification struct {
	Retryable      bool
	ErrorCode      string
	TerminalReason string
}

type RetryDecision struct {
	AttemptCount   int
	ErrorCode      string
	NextRetryAt    *time.Time
	TerminalReason string
}

func retryJitterSample() float64 { return rand.Float64() }

func ClassifyRetryError(err error) RetryClassification {
	if err == nil {
		return RetryClassification{ErrorCode: "operation.unknown_failure", TerminalReason: "unknown_failure"}
	}
	if errors.Is(err, context.Canceled) {
		return RetryClassification{ErrorCode: "operation.cancelled", TerminalReason: "operation_cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return RetryClassification{Retryable: true, ErrorCode: "dependency.timeout"}
	}
	var retryError interface {
		Retryable() bool
		ErrorCode() string
	}
	if errors.As(err, &retryError) {
		classification := RetryClassification{Retryable: retryError.Retryable(), ErrorCode: retryError.ErrorCode()}
		if !classification.Retryable {
			classification.TerminalReason = "terminal_dependency_failure"
		}
		return classification
	}
	if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) {
		return RetryClassification{ErrorCode: "kubernetes.authorization_failed", TerminalReason: "authorization_failure"}
	}
	if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
		return RetryClassification{ErrorCode: "kubernetes.validation_failed", TerminalReason: "validation_failure"}
	}
	if apierrors.IsTooManyRequests(err) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) {
		return RetryClassification{Retryable: true, ErrorCode: "kubernetes.transient_failure"}
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		if strings.HasPrefix(databaseError.Code, "08") || databaseError.Code == "40001" || databaseError.Code == "40P01" || databaseError.Code == "55P03" || databaseError.Code == "53300" || databaseError.Code == "57P01" || databaseError.Code == "57P02" || databaseError.Code == "57P03" {
			return RetryClassification{Retryable: true, ErrorCode: "database.transient_failure"}
		}
		return RetryClassification{ErrorCode: "database.operation_failed", TerminalReason: "database_failure_requires_review"}
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return RetryClassification{Retryable: true, ErrorCode: "database.connection_failed"}
	}
	if errors.Is(err, net.ErrClosed) {
		return RetryClassification{Retryable: true, ErrorCode: "dependency.connection_failed"}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return RetryClassification{Retryable: true, ErrorCode: "dependency.connection_failed"}
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"connection refused", "connection reset", "connection aborted", "no such host", "network is unreachable", "i/o timeout", "tls handshake timeout", "temporary failure", "temporarily unavailable", "remote end hung up", "unexpected eof", "dial tcp"} {
		if strings.Contains(lower, marker) {
			return RetryClassification{Retryable: true, ErrorCode: "dependency.connection_failed"}
		}
	}
	var stageError interface{ ErrorStage() string }
	if errors.As(err, &stageError) {
		switch stageError.ErrorStage() {
		case "git":
			return RetryClassification{ErrorCode: "git.source_unavailable", TerminalReason: "git_source_requires_review"}
		case "render":
			return RetryClassification{ErrorCode: "render.validation_failed", TerminalReason: "render_configuration_requires_review"}
		case "cluster":
			return RetryClassification{ErrorCode: "kubernetes.operation_failed", TerminalReason: "kubernetes_failure_requires_review"}
		}
	}
	return RetryClassification{ErrorCode: "operation.validation_failed", TerminalReason: "validation_failure"}
}

func ComputeRetryDelay(policy store.RetryPolicy, attempt int, jitterSample float64) time.Duration {
	policy = store.NormalizeRetryPolicy(policy)
	if attempt < 1 {
		attempt = 1
	}
	if jitterSample < 0 {
		jitterSample = 0
	} else if jitterSample > 1 {
		jitterSample = 1
	}
	delay := float64(policy.InitialDelaySeconds)
	maximum := float64(policy.MaxDelaySeconds)
	for step := 1; step < attempt && delay < maximum; step++ {
		delay = math.Min(delay*2, maximum)
	}
	spread := float64(policy.JitterPercent) / 100
	delay *= 1 + ((jitterSample*2)-1)*spread
	delay = math.Max(0, math.Min(delay, maximum))
	return time.Duration(delay * float64(time.Second))
}

func DecideRetry(ctx context.Context, policy store.RetryPolicy, attempt int, err error, now time.Time, jitterSample float64, automatic bool) RetryDecision {
	if attempt < 1 {
		attempt = 1
	}
	classification := ClassifyRetryError(err)
	decision := RetryDecision{AttemptCount: attempt, ErrorCode: classification.ErrorCode, TerminalReason: classification.TerminalReason}
	if !classification.Retryable {
		return decision
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		decision.ErrorCode = "operation.cancelled"
		decision.TerminalReason = "operation_cancelled"
		return decision
	}
	if !automatic {
		decision.TerminalReason = "manual_retry_required"
		return decision
	}
	policy = store.NormalizeRetryPolicy(policy)
	if !policy.Enabled {
		decision.TerminalReason = "retry_disabled"
		return decision
	}
	if attempt >= policy.MaxAttempts {
		decision.TerminalReason = "max_attempts_exceeded"
		return decision
	}
	next := now.Add(ComputeRetryDelay(policy, attempt, jitterSample))
	decision.NextRetryAt = &next
	return decision
}
