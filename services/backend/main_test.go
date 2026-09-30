package main

import (
	"bytes"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"log/slog"
	"strings"
	"testing"
)

func TestStartupConfigurationLog(t *testing.T) {
	t.Setenv("JUSTCD_DATABASE_URL", "")
	var output bytes.Buffer
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
	err := run(logger)
	if err == nil {
		t.Fatal("expected startup error")
	}
	logger.Error("JustCD stopped", "error", observability.StartupError{Err: err})
	if !strings.Contains(output.String(), "load configuration: JUSTCD_DATABASE_URL is required") {
		t.Fatalf("missing cause: %s", output.String())
	}
}
