package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const apiErrorSchemaVersion = 1

type apiErrorMetadata struct {
	code           string
	category       string
	retryable      bool
	remediation    string
	remediationURL string
}

var (
	bearerCredentialPattern = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`)
	inlineCredentialPattern = regexp.MustCompile(`(?i)((?:["']?(?:access[_-]?token|refresh[_-]?token|api[_-]?key|token|password|secret|authorization|cookie|credential|private[_-]?key|kubeconfig|known[_-]?hosts|client[_-]?certificate[_-]?data|certificate[_-]?authority[_-]?data|client[_-]?key[_-]?data)["']?)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
	urlCredentialPattern    = regexp.MustCompile(`(?i)((?:https?|ssh|git)://)[^/@\s]+:[^/@\s]+@`)
	privateKeyPattern       = regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?-----END [^-\r\n]*PRIVATE KEY-----`)
	sensitiveDetailKey      = regexp.MustCompile(`(?i)(token|secret|password|authorization|cookie|private.?key|api.?key|credential|kubeconfig|known.?hosts|certificate.?data|client.?key.?data|csrf|session)`)
)

func classifyAPIError(status int) apiErrorMetadata {
	switch status {
	case http.StatusBadRequest:
		return apiErrorMetadata{"request.invalid", "validation", false, "Review the submitted values and correct the invalid fields.", ""}
	case http.StatusUnauthorized:
		return apiErrorMetadata{"authentication.required", "authentication", false, "Sign in again to continue.", "/login"}
	case http.StatusForbidden:
		return apiErrorMetadata{"authorization.denied", "authorization", false, "Confirm that your account has the required project role or administrator access.", ""}
	case http.StatusNotFound:
		return apiErrorMetadata{"resource.not_found", "not_found", false, "Refresh the page and confirm that the resource still exists.", ""}
	case http.StatusRequestTimeout:
		return apiErrorMetadata{"request.timeout", "request", false, "Refresh the current state before trying this request again.", ""}
	case http.StatusConflict:
		return apiErrorMetadata{"state.conflict", "conflict", false, "Refresh and review the latest state before trying again.", ""}
	case http.StatusUnprocessableEntity:
		return apiErrorMetadata{"request.rejected", "validation", false, "Review the affected resource and correct its configuration before retrying.", ""}
	case http.StatusTooManyRequests:
		return apiErrorMetadata{"request.rate_limited", "rate_limit", true, "Wait briefly, then retry the request.", ""}
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return apiErrorMetadata{"dependency.unavailable", "dependency", true, "Check the connected service and retry when it is available.", ""}
	default:
		if status >= http.StatusInternalServerError {
			return apiErrorMetadata{"internal.error", "internal", false, "Retry after checking JustCD service health. If the problem continues, contact an administrator.", ""}
		}
		return apiErrorMetadata{"request.failed", "request", false, "Review the request and try again.", ""}
	}
}

func redactAPIErrorMessage(message string) string {
	message = bearerCredentialPattern.ReplaceAllString(message, `${1}[redacted]`)
	message = inlineCredentialPattern.ReplaceAllString(message, `${1}[redacted]`)
	message = urlCredentialPattern.ReplaceAllString(message, `${1}[redacted]@`)
	message = privateKeyPattern.ReplaceAllString(message, "[redacted private key]")
	if len(message) > 4000 {
		message = message[:4000] + "…"
	}
	return message
}

func writeError(w http.ResponseWriter, status int, message string) {
	metadata := classifyAPIError(status)
	writeAPIError(w, status, message, metadata, nil)
}

func writeClassifiedError(w http.ResponseWriter, status int, message string, metadata apiErrorMetadata, details map[string]any) {
	writeAPIError(w, status, message, metadata, details)
}

func writeErrorWithDetails(w http.ResponseWriter, status int, message string, details map[string]any) {
	metadata := classifyAPIError(status)
	writeAPIError(w, status, message, metadata, details)
}

func writeStoreError(w http.ResponseWriter, message string) {
	writeClassifiedError(w, http.StatusInternalServerError, message, apiErrorMetadata{
		code:        "database.operation_failed",
		category:    "database",
		retryable:   false,
		remediation: "Check PostgreSQL availability and backend logs, then retry only after resolving the cause.",
	}, nil)
}

func writePlanFailure(w http.ResponseWriter, status int, message, applicationID string, err error) {
	stage := "plan"
	var staged interface{ ErrorStage() string }
	if errors.As(err, &staged) {
		stage = staged.ErrorStage()
	}

	metadata := apiErrorMetadata{
		code:        "plan.calculation_failed",
		category:    "plan",
		retryable:   false,
		remediation: "Check the application source, target bindings, and current cluster state, then build a fresh plan.",
	}
	details := map[string]any{"stage": stage}
	if applicationID != "" {
		details["applicationId"] = applicationID
		metadata.remediationURL = "/applications/" + url.PathEscape(applicationID) + "/edit"
	}

	switch stage {
	case "git":
		metadata.code = "git.fetch_failed"
		metadata.category = "git"
		metadata.remediation = "Check the repository URL, selected revision, credentials, and Git provider availability."
		metadata.remediationURL = "/settings/connections"
	case "render":
		metadata.code = "render.failed"
		metadata.category = "render"
		metadata.remediation = "Check the manifest path, renderer settings, values files, and Kubernetes object definitions."
	case "cluster":
		metadata.code = "kubernetes.plan_check_failed"
		metadata.category = "kubernetes"
		metadata.remediation = "Check cluster connectivity, credential scope, namespace bindings, and Kubernetes RBAC permissions."
		metadata.remediationURL = "/settings/connections"
	}

	writeClassifiedError(w, status, message, metadata, details)
}

func writeAPIError(w http.ResponseWriter, status int, message string, metadata apiErrorMetadata, details map[string]any) {
	payload := map[string]any{
		"schemaVersion": apiErrorSchemaVersion,
		"error":         redactAPIErrorMessage(message),
		"code":          metadata.code,
		"category":      metadata.category,
		"retryable":     metadata.retryable,
		"remediation":   metadata.remediation,
	}
	if metadata.remediationURL != "" {
		payload["remediationUrl"] = metadata.remediationURL
	}
	if len(details) > 0 {
		safeDetails := redactAPIErrorDetails(details)
		payload["details"] = safeDetails
		// Keep legacy operation-specific fields at the top level while new
		// clients use the versioned details object.
		for key, value := range safeDetails {
			switch key {
			case "schemaVersion", "error", "code", "category", "retryable", "remediation", "remediationUrl", "details":
				continue
			default:
				payload[key] = value
			}
		}
	}
	writeJSON(w, status, payload)
}

func redactAPIErrorDetails(details map[string]any) map[string]any {
	encoded, err := json.Marshal(details)
	if err != nil {
		return map[string]any{"diagnostics": "Structured error details were omitted because they could not be safely encoded."}
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return map[string]any{"diagnostics": "Structured error details were omitted because they could not be safely decoded."}
	}
	return redactAPIErrorValue(decoded, false).(map[string]any)
}

func redactAPIErrorValue(value any, inKubernetesSecret bool) any {
	switch value := value.(type) {
	case map[string]any:
		isKubernetesSecret := inKubernetesSecret || strings.EqualFold(apiErrorStringValue(value["kind"]), "Secret")
		redacted := make(map[string]any, len(value))
		for key, item := range value {
			if sensitiveDetailKey.MatchString(key) || (isKubernetesSecret && (strings.EqualFold(key, "data") || strings.EqualFold(key, "stringData"))) {
				redacted[key] = "[redacted]"
				continue
			}
			redacted[key] = redactAPIErrorValue(item, isKubernetesSecret)
		}
		return redacted
	case []any:
		redacted := make([]any, len(value))
		for i, item := range value {
			redacted[i] = redactAPIErrorValue(item, inKubernetesSecret)
		}
		return redacted
	case string:
		return redactAPIErrorMessage(value)
	default:
		return value
	}
}

func apiErrorStringValue(value any) string {
	text, _ := value.(string)
	return text
}
