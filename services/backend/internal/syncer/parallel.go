package syncer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
)

// DefaultConcurrency bounds simultaneous Kubernetes requests made for one
// application. Each request through a cluster agent is a queued round trip, so
// sequential work over a large application dominated plan and sync time.
const DefaultConcurrency = 8

// MaxConcurrency keeps a misconfigured value from flooding an API server.
const MaxConcurrency = 32

func (s *Service) concurrency() int {
	if s == nil || s.Concurrency <= 0 {
		return DefaultConcurrency
	}
	return min(s.Concurrency, MaxConcurrency)
}

// forEachLimit runs fn for indexes 0..n-1 with at most limit calls in flight.
// It starts no new work after a failure and returns the error of the lowest
// failing index. Indexes start in order, so every lower index has run and the
// reported failure matches what a sequential loop would have returned. A limit
// of one is exactly the sequential loop.
func forEachLimit(n, limit int, fn func(int) error) error {
	if n == 0 {
		return nil
	}
	if limit < 1 {
		limit = 1
	}
	if limit == 1 || n == 1 {
		for i := range n {
			if err := fn(i); err != nil {
				return err
			}
		}
		return nil
	}
	errs := make([]error, n)
	var failed atomic.Bool
	var wg sync.WaitGroup
	slots := make(chan struct{}, limit)
	for i := range n {
		slots <- struct{}{}
		if failed.Load() {
			<-slots
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			if err := fn(i); err != nil {
				errs[i] = err
				failed.Store(true)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Stage names recorded by beginStage. Keep this vocabulary small: it becomes a
// Prometheus label.
const (
	stageGitFetch        = "git_fetch"
	stageRender          = "render"
	stageLiveSnapshot    = "live_snapshot"
	stageDryRun          = "dry_run"
	stageRecheck         = "recheck"
	stageCheckpoint      = "checkpoint"
	stageApply           = "apply"
	stageDelete          = "delete"
	stageSuccessSnapshot = "success_snapshot"
)

// beginStage opens a trace span and returns a function that ends it and
// records the stage duration, so operators can see where a sync spends time.
func (s *Service) beginStage(ctx context.Context, stage string) (context.Context, func()) {
	ctx, span := otel.Tracer("justcd/syncer").Start(ctx, "sync."+stage)
	started := time.Now()
	return ctx, func() {
		span.End()
		if s != nil {
			s.Metrics.RecordSyncStage(stage, time.Since(started))
		}
	}
}
