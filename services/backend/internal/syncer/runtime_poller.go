package syncer

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Runtime observation is independent of Git polling, sync pause, retries and
// operation leases. It only reads Kubernetes and never reconciles workloads.
func (s *Service) RunRuntimePoller(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		applications, err := s.Store.DueRuntimeApplications(ctx, 25)
		if err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "could not list runtime observations", "error", err)
		}
		// Bound concurrency so an unreachable cluster does not stall every
		// other application, without flooding Kubernetes or the database pool.
		workers := make(chan struct{}, 4)
		var group sync.WaitGroup
	observe:
		for _, app := range applications {
			if ctx.Err() != nil {
				break
			}
			select {
			case workers <- struct{}{}:
			case <-ctx.Done():
				break observe
			}
			group.Add(1)
			go func() {
				defer group.Done()
				defer func() { <-workers }()
				observeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				defer cancel()
				if err := s.RefreshApplicationHealth(observeCtx, app); err != nil && ctx.Err() == nil {
					logger.WarnContext(ctx, "runtime observation incomplete", "applicationId", app.ID, "error", err)
				}
			}()
		}
		group.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
