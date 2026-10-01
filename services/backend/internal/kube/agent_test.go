package kube

import (
	"net/http/httptest"
	"testing"
)

func TestUnknownWritesRequireReview(t *testing.T) {
	for _, tc := range []struct {
		method, uri string
		transient   bool
	}{{"PATCH", "/api/v1/namespaces/dev/configmaps/x", false}, {"PATCH", "/api/v1/namespaces/dev/configmaps/x?dryRun=All", true}, {"GET", "/api/v1/namespaces/dev/configmaps/x", true}, {"POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", true}} {
		r := httptest.NewRequest(tc.method, tc.uri, nil)
		err := uncertainAgentError(r, "request outcome unknown").(*AgentError)
		if err.Retryable() != tc.transient {
			t.Fatalf("%s %s incorrectly retryable", tc.method, tc.uri)
		}
	}
}
