package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
		logger.Error("JustCD stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStartup()
	telemetry, err := observability.Init(startupCtx, cfg.Observability)
	if err != nil {
		return err
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
		return err
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
	httpServer := &http.Server{
		Addr: cfg.ListenAddress, Handler: observability.HTTPMiddleware(server, logger, telemetry.Metrics),
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
	go server.Syncer.RunPoller(signalCtx, logger)
	go server.Syncer.RunRepositoryPoller(signalCtx, logger)
	go server.Syncer.RunOperationWorker(signalCtx, logger)
	go server.RunReviewWorker(signalCtx, logger)
	if telemetry.Metrics.Enabled() {
		go runQueueMetrics(signalCtx, db, telemetry.Metrics, logger)
	}
	select {
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
