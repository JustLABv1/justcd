package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/justlab/justcd/services/backend/internal/api"
	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func main() {
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(logger); err != nil {
		logger.Error("JustCD stopped", "error", observability.StartupError{Err: err})
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStartup()
	telemetry, err := observability.Init(startupCtx, cfg.Observability)
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "could not flush telemetry", "error", err)
		}
	}()
	db, err := store.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.DB.Close()
	if err := db.EnsureSystemActor(startupCtx); err != nil {
		return fmt.Errorf("initialize automatic reconciliation actor: %w", err)
	}
	server, err := api.New(db, cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize API: %w", err)
	}
	server.Metrics = telemetry.Metrics
	server.Syncer.Metrics = telemetry.Metrics
	if telemetry.Metrics.Enabled() {
		server.Mux.Handle("GET /metrics", telemetry.Metrics.Handler())
	}
	lifecycle := newBackendLifecycle()
	lifecycle.Start(func(ctx, _ context.Context) { server.RunNotificationWorker(ctx) })
	httpServer := &http.Server{
		Addr: cfg.ListenAddress, Handler: lifecycle.Handler(observability.HTTPMiddleware(server, logger, telemetry.Metrics)),
		BaseContext:       func(net.Listener) context.Context { return lifecycle.executionCtx },
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 90 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("JustCD API listening", "address", cfg.ListenAddress)
		serveErr <- httpServer.ListenAndServe()
	}()
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	lifecycle.Start(func(ctx, _ context.Context) { server.Syncer.RunPoller(ctx, logger) })
	lifecycle.Start(func(ctx, _ context.Context) { server.Syncer.RunRepositoryPoller(ctx, logger) })
	lifecycle.Start(func(ctx, executionCtx context.Context) {
		server.Syncer.RunOperationWorkerWithDrain(ctx, executionCtx, logger)
	})
	lifecycle.Start(func(ctx, executionCtx context.Context) {
		server.RunTaskWorker(ctx, executionCtx, logger)
	})
	lifecycle.Start(func(ctx, _ context.Context) { server.RunReviewWorker(ctx, logger) })
	if telemetry.Metrics.Enabled() {
		lifecycle.Start(func(ctx, _ context.Context) { runQueueMetrics(ctx, db, telemetry.Metrics, logger) })
	}
	var serveResult error
	select {
	case <-signalCtx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			serveResult = fmt.Errorf("serve HTTP on %s: %w", cfg.ListenAddress, err)
		}
	}
	logger.Info("JustCD backend draining", "gracePeriod", cfg.ShutdownGracePeriod)
	return errors.Join(serveResult, lifecycle.Shutdown(httpServer, cfg.ShutdownGracePeriod))
}

func runQueueMetrics(ctx context.Context, db *store.Store, metrics *observability.Metrics, logger *slog.Logger) {
	update := func() {
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		queued, running, err := db.OperationQueueDepth(readCtx)
		if err != nil {
			logger.WarnContext(ctx, "could not refresh operation queue metrics", "error", err)
			return
		}
		metrics.SetQueueDepth(queued, running)
	}
	update()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}
