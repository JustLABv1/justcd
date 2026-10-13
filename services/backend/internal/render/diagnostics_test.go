package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/observability"
)

func TestRendererDiagnosticCommand(t *testing.T) {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[len(os.Args)-1], "renderer-diagnostic-helper:") {
		return
	}
	mode := strings.TrimPrefix(os.Args[len(os.Args)-1], "renderer-diagnostic-helper:")
	fmt.Fprint(os.Stdout, "stdout-secret-canary")
	switch mode {
	case "invalid-patch":
		fmt.Fprintln(os.Stderr, "Error: unable to find patch target Deployment/api")
		fmt.Fprintln(os.Stderr, "https://user:url-canary@example.com password=password-canary Authorization: Bearer bearer-canary")
		fmt.Fprintln(os.Stderr, "-----BEGIN PRIVATE KEY-----\nkey-canary\n-----END PRIVATE KEY-----")
	case "network":
		fmt.Fprintln(os.Stderr, "connection refused")
	case "long":
		fmt.Fprint(os.Stderr, "password="+strings.Repeat("secret-canary", 500)+"; "+strings.Repeat("x", 5000))
	}
	os.Exit(7)
}

func TestRendererFailureLogsDiagnosticsWithoutChangingPublicError(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"invalid-patch", "network", "empty", "long"} {
		t.Run(mode, func(t *testing.T) {
			_, err := runRenderer(context.Background(), executable, []string{"-test.run=^TestRendererDiagnosticCommand$", "--", "renderer-diagnostic-helper:" + mode}, t.TempDir())
			var rendererError *RendererError
			if !errors.As(err, &rendererError) {
				t.Fatalf("expected renderer error, got %v", err)
			}
			if rendererError.ErrorCode() != "render.command_failed" || rendererError.Retryable() != (mode == "network") {
				t.Fatalf("incorrect classification: %+v", rendererError)
			}
			if rendererError.Error() != executable+" renderer failed" {
				t.Fatalf("public error contains diagnostics: %s", rendererError.Error())
			}
			// Mimic the wrapped error sent to the plan calculation logger.
			wrapped := fmt.Errorf("render namespace production: %w", err)
			var output bytes.Buffer
			logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
			logger.Warn("plan calculation failed", "error", wrapped)
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			message, _ := entry["error"].(string)
			if !strings.Contains(message, "render namespace production:") || !strings.Contains(message, "exit status 7") {
				t.Fatalf("missing command diagnostic: %s", message)
			}
			if mode == "invalid-patch" && !strings.Contains(message, "unable to find patch target Deployment/api") {
				t.Fatalf("missing stderr: %s", message)
			}
			for _, secret := range []string{"stdout-secret-canary", "url-canary", "password-canary", "bearer-canary", "key-canary", "secret-canary"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("leaked %q: %s", secret, output.String())
				}
			}
			if mode == "long" && (len(message) > 4003 || !strings.HasSuffix(message, "…")) {
				t.Fatalf("stderr was not limited: %d bytes", len(message))
			}
		})
	}
}

func TestRendererFailureLogsCommandStartError(t *testing.T) {
	_, err := runRenderer(context.Background(), "/nonexistent/justcd-renderer", nil, t.TempDir())
	var rendererError *RendererError
	if !errors.As(err, &rendererError) || !strings.Contains(rendererError.LogDiagnostic(), "no such file or directory") {
		t.Fatalf("missing command start diagnostic: %v", err)
	}
}
