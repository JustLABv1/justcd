package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func awaitShutdownSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown checkpoint timed out")
	}
}

func TestShutdownDrainsBeforeCancellingExecution(t *testing.T) {
	l := newBackendLifecycle()
	t.Cleanup(l.cancelExecution)
	t.Cleanup(l.stopAcquiring)
	stopped, release := make(chan struct{}), make(chan struct{})
	l.Start(func(acquireCtx, executionCtx context.Context) {
		<-acquireCtx.Done()
		if executionCtx.Err() != nil {
			t.Error("execution cancelled before drain")
		}
		close(stopped)
		<-release
	})
	result := make(chan error, 1)
	go func() { result <- l.Shutdown(&http.Server{}, time.Second) }()
	awaitShutdownSignal(t, stopped)
	select {
	case err := <-result:
		t.Fatalf("shutdown returned before worker finished: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join worker")
	}
}

func TestShutdownDeadlineCancelsAndWaitsForCleanup(t *testing.T) {
	l := newBackendLifecycle()
	t.Cleanup(l.cancelExecution)
	t.Cleanup(l.stopAcquiring)
	cancelled, cleanup := make(chan struct{}), make(chan struct{})
	l.Start(func(_, executionCtx context.Context) {
		<-executionCtx.Done()
		close(cancelled)
		<-cleanup
	})
	result := make(chan error, 1)
	go func() { result <- l.Shutdown(&http.Server{}, 20*time.Millisecond) }()
	awaitShutdownSignal(t, cancelled)
	select {
	case err := <-result:
		t.Fatalf("shutdown returned before cancellation cleanup: %v", err)
	default:
	}
	close(cleanup)
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected drain timeout, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join cancelled worker")
	}
}

func TestShutdownJoinsHandlersAndRejectsNewRequests(t *testing.T) {
	l := newBackendLifecycle()
	t.Cleanup(l.cancelExecution)
	t.Cleanup(l.stopAcquiring)
	started, release := make(chan struct{}), make(chan struct{})
	handler := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	awaitShutdownSignal(t, started)
	result := make(chan error, 1)
	go func() { result <- l.Shutdown(&http.Server{}, time.Second) }()
	awaitShutdownSignal(t, l.acquireCtx.Done())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("new request admitted during shutdown: %d", response.Code)
	}
	select {
	case err := <-result:
		t.Fatalf("shutdown returned before handler finished: %v", err)
	default:
	}
	close(release)
	awaitShutdownSignal(t, handled)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineCancelsHTTPRequests(t *testing.T) {
	l := newBackendLifecycle()
	t.Cleanup(l.cancelExecution)
	t.Cleanup(l.stopAcquiring)
	started, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	})))
	server.Config.BaseContext = func(net.Listener) context.Context { return l.executionCtx }
	server.Start()
	t.Cleanup(server.Close)
	requested := make(chan struct{})
	go func() {
		defer close(requested)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	awaitShutdownSignal(t, started)
	if err := l.Shutdown(server.Config, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected HTTP drain timeout, got %v", err)
	}
	awaitShutdownSignal(t, cancelled)
	awaitShutdownSignal(t, requested)
}
