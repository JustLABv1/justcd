package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestPlanFailureReportsKubernetesPermissionDenial(t *testing.T) {
	response := httptest.NewRecorder()
	err := fmt.Errorf("wrapped: %w", apierrors.NewForbidden(schema.GroupResource{Group: "secrets.hashicorp.com", Resource: "vaultstaticsecrets"}, "database", fmt.Errorf("bearer private-token")))
	writePlanFailure(response, http.StatusUnprocessableEntity, "could not calculate a safe plan", "app-1", err)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "kubernetes.permission_denied" || body["category"] != "kubernetes" || body["remediationUrl"] != "/applications/app-1?tab=settings" {
		t.Fatalf("RBAC diagnosis lost: %#v", body)
	}
	if !strings.Contains(body["remediation"].(string), "exclude") || strings.Contains(response.Body.String(), "private-token") {
		t.Fatalf("unsafe or unhelpful RBAC response: %s", response.Body.String())
	}
}

func TestWriteErrorUsesStableVersionedEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		message   string
		code      string
		category  string
		retryable bool
	}{
		{name: "validation", status: http.StatusBadRequest, message: "invalid workspace name", code: "request.invalid", category: "validation"},
		{name: "authentication", status: http.StatusUnauthorized, message: "authentication required", code: "authentication.required", category: "authentication"},
		{name: "authorization", status: http.StatusForbidden, message: "workspace access denied", code: "authorization.denied", category: "authorization"},
		{name: "not found", status: http.StatusNotFound, message: "application not found", code: "resource.not_found", category: "not_found"},
		{name: "conflict", status: http.StatusConflict, message: "plan is stale", code: "state.conflict", category: "conflict"},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, message: "invalid manifest", code: "request.rejected", category: "validation"},
		{name: "rate limit", status: http.StatusTooManyRequests, message: "too many attempts", code: "request.rate_limited", category: "rate_limit", retryable: true},
		{name: "upstream", status: http.StatusBadGateway, message: "provider unavailable", code: "dependency.unavailable", category: "dependency", retryable: true},
		{name: "internal", status: http.StatusInternalServerError, message: "service failed", code: "internal.error", category: "internal"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeError(response, test.status, test.message)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("content type = %q", contentType)
			}

			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["schemaVersion"] != float64(apiErrorSchemaVersion) || body["error"] != test.message {
				t.Fatalf("versioned legacy-compatible fields missing: %#v", body)
			}
			if body["code"] != test.code || body["category"] != test.category || body["retryable"] != test.retryable {
				t.Fatalf("error metadata = %#v", body)
			}
			if remediation, ok := body["remediation"].(string); !ok || remediation == "" {
				t.Fatalf("remediation missing: %#v", body)
			}
		})
	}
}

func TestWriteStoreErrorUsesDatabaseCategory(t *testing.T) {
	response := httptest.NewRecorder()
	writeStoreError(response, "could not load workspaces")

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "database.operation_failed" || body["category"] != "database" || body["retryable"] != false {
		t.Fatalf("database metadata = %#v", body)
	}
}

func TestWriteClassifiedErrorKeepsLegacyFieldsAndVersionedDetails(t *testing.T) {
	response := httptest.NewRecorder()
	writeClassifiedError(response, http.StatusConflict, "ownership conflict", apiErrorMetadata{
		code:        "resource.ownership_conflict",
		category:    "kubernetes",
		retryable:   false,
		remediation: "Review the existing resource before adopting it.",
	}, map[string]any{"conflict": map[string]string{"name": "web"}, "conflicts": []string{"web"}})

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "resource.ownership_conflict" {
		t.Fatalf("code = %#v", body["code"])
	}
	if _, ok := body["conflict"]; !ok {
		t.Fatalf("legacy conflict field missing: %#v", body)
	}
	details, ok := body["details"].(map[string]any)
	if !ok || details["conflicts"] == nil {
		t.Fatalf("structured details missing: %#v", body["details"])
	}
}

func TestWritePlanFailureClassifiesStageAndAddsContextualRemediation(t *testing.T) {
	tests := []struct {
		stage    string
		code     string
		category string
		url      string
	}{
		{stage: "git", code: "git.fetch_failed", category: "git", url: "/settings/connections"},
		{stage: "render", code: "render.failed", category: "render", url: "/applications/app-1/edit"},
		{stage: "cluster", code: "kubernetes.plan_check_failed", category: "kubernetes", url: "/settings/connections"},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			response := httptest.NewRecorder()
			wrapped := fmt.Errorf("plan calculation failed: %w", testStageError{stage: test.stage})
			writePlanFailure(response, http.StatusUnprocessableEntity, "could not calculate a safe plan", "app-1", wrapped)

			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["code"] != test.code || body["category"] != test.category || body["remediationUrl"] != test.url {
				t.Fatalf("metadata = %#v", body)
			}
			details := body["details"].(map[string]any)
			if details["stage"] != test.stage {
				t.Fatalf("stage details = %#v", details)
			}
			if details["applicationId"] != "app-1" {
				t.Fatalf("application details = %#v", details)
			}
		})
	}
}

func TestWriteClassifiedErrorRedactsNestedCredentialsAndKubernetesSecrets(t *testing.T) {
	response := httptest.NewRecorder()
	writeClassifiedError(response, http.StatusBadGateway, "operation failed", apiErrorMetadata{
		code:        "sync.apply_failed",
		category:    "kubernetes",
		remediation: "Inspect the operation and resolve the failed resource.",
	}, map[string]any{
		"operation": map[string]any{
			"message": "clone https://deployer:repo-password@example.test failed",
			"cause": []any{
				map[string]any{"authorization": "Bearer nested-bearer-token", "safeContext": "kept"},
			},
			"resource": map[string]any{
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "registry-auth"},
				"data":       map[string]string{".dockerconfigjson": "base64-secret-value"},
				"stringData": map[string]string{"password": "plain-secret-value"},
			},
		},
	})

	body := response.Body.String()
	for _, secret := range []string{"repo-password", "nested-bearer-token", "base64-secret-value", "plain-secret-value"} {
		if strings.Contains(body, secret) {
			t.Fatalf("nested error details leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "safeContext") || !strings.Contains(body, "registry-auth") || !strings.Contains(body, "[redacted]") {
		t.Fatalf("safe diagnostic context should remain available: %s", body)
	}
}

type testStageError struct{ stage string }

func (e testStageError) Error() string      { return "stage failed" }
func (e testStageError) ErrorStage() string { return e.stage }

func TestRedactAPIErrorMessageRemovesCredentialMaterial(t *testing.T) {
	message := "clone https://deploy:private-value@example.test failed; Authorization: Bearer bearer-value; password=plain-value; kubeconfig: kube-config-value client-key-data: client-key-value; -----BEGIN PRIVATE KEY-----\nkey-material\n-----END PRIVATE KEY-----"
	redacted := redactAPIErrorMessage(message)
	for _, secret := range []string{"private-value", "bearer-value", "plain-value", "kube-config-value", "client-key-value", "key-material"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("error response leaked %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "[redacted]") || !strings.Contains(redacted, "[redacted private key]") {
		t.Fatalf("redaction marker missing: %s", redacted)
	}
}

func TestRequestLoggingIncludesSafeErrorClassification(t *testing.T) {
	for _, status := range []int{200, 400, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v1/clusters/{clusterID}/tests", func(w http.ResponseWriter, r *http.Request) {
				if status == 200 {
					writeJSON(w, status, map[string]string{"status": "ok"})
					return
				}
				if status == 500 {
					writeStoreError(w, "could not load Kubernetes permission reports")
					return
				}
				writeError(w, status, "failed")
			})
			response := httptest.NewRecorder()
			observability.HTTPMiddleware(mux, logger, nil).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/clusters/private-id/tests?workspaceId=private-workspace&token=private-token", nil))
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			level := "INFO"
			if status >= 500 {
				level = "ERROR"
			}
			if entry["level"] != level || entry["route"] != "GET /api/v1/clusters/{clusterID}/tests" || entry["status"] != float64(status) || entry["request_id"] != response.Header().Get("X-Request-ID") {
				t.Fatalf("incorrect request log: %s", output.String())
			}
			if status == 500 && (entry["error_code"] != "database.operation_failed" || entry["error_category"] != "database") {
				t.Fatalf("missing classification: %s", output.String())
			}
			for _, secret := range []string{"private-id", "private-workspace", "private-token", "could not load"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("log included response or request data: %s", output.String())
				}
			}
		})
	}
}
