package clusteragent

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type credentialFixture struct {
	mu                          sync.Mutex
	uid                         string
	granted                     time.Time
	issueStatus, workloadStatus int
	issuance, workloads         int
	tokens                      map[string]bool
	replaceOnIssue              bool
	accountUnavailable          bool
}

func newCredentialFixture(t *testing.T) (*Agent, *credentialFixture) {
	t.Helper()
	f := &credentialFixture{uid: "account-1", granted: time.Now().Add(5 * time.Minute), tokens: map[string]bool{}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		base := "/api/v1/namespaces/dev/serviceaccounts/justcd-deployer"
		switch r.URL.Path {
		case base:
			if r.Header.Get("Authorization") != "Bearer issuer-secret" {
				t.Error("account read did not use issuer")
				w.WriteHeader(401)
				return
			}
			if f.accountUnavailable {
				w.WriteHeader(503)
				return
			}
			_ = json.NewEncoder(w).Encode(corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "justcd-deployer", Namespace: "dev", UID: types.UID(f.uid)}})
		case base + "/token":
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer issuer-secret" {
				t.Error("issuance did not use issuer")
				w.WriteHeader(401)
				return
			}
			var request authenticationv1.TokenRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Spec.ExpirationSeconds == nil || *request.Spec.ExpirationSeconds != 600 || len(request.Spec.Audiences) != 1 || request.Spec.Audiences[0] != "api-audience" {
				t.Error("issuance policy not honored")
			}
			f.issuance++
			if f.issueStatus != 0 {
				w.WriteHeader(f.issueStatus)
				_, _ = w.Write([]byte(`{"status":{"token":"never-expose-this-token"}}`))
				return
			}
			token := fmt.Sprintf("deployment-secret-%d", f.issuance)
			f.tokens["Bearer "+token] = true
			if f.replaceOnIssue {
				f.uid = "replacement"
			}
			_ = json.NewEncoder(w).Encode(authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: token, ExpirationTimestamp: metav1.NewTime(f.granted)}})
		default:
			f.workloads++
			if !f.tokens[r.Header.Get("Authorization")] || r.Header.Get("X-Issuer-Transport") != "" || r.Header.Get("Impersonate-User") != "" {
				t.Error("workload did not use isolated minted-token authentication")
				w.WriteHeader(401)
				return
			}
			if f.workloadStatus != 0 {
				w.WriteHeader(f.workloadStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"kind":"ConfigMap","metadata":{"name":"test"}}`))
		}
	}))
	t.Cleanup(server.Close)
	kube := &rest.Config{Host: server.URL, BearerToken: "issuer-secret", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	kube.WrapTransport = func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			copy := r.Clone(r.Context())
			copy.Header.Set("X-Issuer-Transport", "issuer-only")
			return next.RoundTrip(copy)
		})
	}
	a, err := New(Config{ServerURL: "https://justcd.invalid", ClusterID: "test-cluster", IdentityFile: t.TempDir() + "/identity", EnrollmentTokenFile: "unused", Profiles: []agentprotocol.Profile{{Name: "managed", WorkspaceIDs: []string{"workspace"}, Namespaces: []string{"dev"}, DeploymentCredentials: &agentprotocol.DeploymentCredentials{LifetimeSeconds: 600, Audience: "api-audience"}}}}, kube)
	if err != nil {
		t.Fatal(err)
	}
	return a, f
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func managedTask() agentprotocol.Request {
	return agentprotocol.Request{ID: "request", Profile: "managed", WorkspaceID: "workspace", Namespace: "dev", Method: "GET", URI: "/api/v1/namespaces/dev/configmaps/test", Deadline: time.Now().Add(time.Minute)}
}
func requireManagedSuccess(t *testing.T, a *Agent, task agentprotocol.Request) {
	t.Helper()
	result := a.Execute(context.Background(), task)
	if result.Error != "" || result.Status != 200 {
		t.Fatalf("managed execution failed: status=%d code=%s error=%s", result.Status, result.ErrorCode, result.Error)
	}
	if strings.Contains(string(result.Body), "secret-") {
		t.Fatal("token exposed in result")
	}
}
func TestManagedCredentialConcurrentRefreshAndUID(t *testing.T) {
	a, f := newCredentialFixture(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { requireManagedSuccess(t, a, managedTask()) })
	}
	wg.Wait()
	f.mu.Lock()
	if f.issuance != 1 || f.workloads != 16 {
		t.Errorf("issuance=%d workloads=%d", f.issuance, f.workloads)
	}
	f.uid = "account-2"
	f.mu.Unlock()
	requireManagedSuccess(t, a, managedTask())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issuance != 2 {
		t.Fatal("replacement account reused previous UID's token")
	}
}
func TestManagedCredentialGrantedExpiryAndRefresh(t *testing.T) {
	a, f := newCredentialFixture(t)
	requireManagedSuccess(t, a, managedTask())
	a.credentials.mu.Lock()
	for key, value := range a.credentials.entries {
		if !value.expires.Equal(f.granted.Truncate(time.Second)) {
			t.Error("cache did not use returned expiry")
		}
		value.refresh = time.Now().Add(-time.Second)
		a.credentials.entries[key] = value
	}
	a.credentials.mu.Unlock()
	task := managedTask()
	task.Method = "PATCH"
	task.URI += "?dryRun=All"
	requireManagedSuccess(t, a, task)
	task = managedTask()
	task.Method = "DELETE"
	requireManagedSuccess(t, a, task)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issuance != 2 || f.workloads != 3 {
		t.Errorf("issuance=%d workloads=%d", f.issuance, f.workloads)
	}
}
func TestManagedCredentialFailureNeverFallsBackOrLeaks(t *testing.T) {
	for _, scenario := range []string{"issuer-forbidden", "issuer-outage", "expired", "too-short", "account-replaced", "account-outage"} {
		t.Run(scenario, func(t *testing.T) {
			a, f := newCredentialFixture(t)
			f.mu.Lock()
			switch scenario {
			case "issuer-forbidden":
				f.issueStatus = 403
			case "issuer-outage":
				f.issueStatus = 503
			case "expired":
				f.granted = time.Now().Add(-time.Minute)
			case "too-short":
				f.granted = time.Now().Add(15 * time.Second)
			case "account-replaced":
				f.replaceOnIssue = true
			case "account-outage":
				f.accountUnavailable = true
			}
			f.mu.Unlock()
			result := a.Execute(context.Background(), managedTask())
			encoded, _ := json.Marshal(result)
			if result.ErrorCode != "cluster.agent_credential_unavailable" || result.Error == "" || strings.Contains(string(encoded), "never-expose") || strings.Contains(string(encoded), "secret-") {
				t.Fatal("unsafe credential failure")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.workloads != 0 {
				t.Fatal("credential failure executed workload")
			}
		})
	}
}
func TestManagedCredentialRejectsEscapesBeforeIssuance(t *testing.T) {
	for _, scenario := range []string{"workspace", "namespace", "cluster-scope", "secrets-other-namespace", "account-token", "other-account-token", "rbac", "namespace-create", "namespace-delete", "cluster-resource", "protected"} {
		t.Run(scenario, func(t *testing.T) {
			a, f := newCredentialFixture(t)
			task := managedTask()
			switch scenario {
			case "workspace":
				task.WorkspaceID = "other"
			case "namespace":
				task.Namespace = "other"
			case "cluster-scope":
				task.ClusterScope = true
			case "secrets-other-namespace":
				task.URI = "/api/v1/namespaces/other/secrets"
			case "account-token":
				task.Method = "POST"
				task.URI = "/api/v1/namespaces/dev/serviceaccounts/justcd-deployer/token"
			case "other-account-token":
				task.Method = "POST"
				task.URI = "/api/v1/namespaces/dev/serviceaccounts/default/token"
			case "rbac":
				task.Method = "POST"
				task.URI = "/apis/rbac.authorization.k8s.io/v1/namespaces/dev/rolebindings"
			case "namespace-create":
				task.Method = "POST"
				task.URI = "/api/v1/namespaces"
			case "namespace-delete":
				task.Method = "DELETE"
				task.URI = "/api/v1/namespaces/dev"
			case "cluster-resource":
				task.URI = "/api/v1/nodes"
			case "protected":
				task.Namespace = "kube-system"
				task.URI = "/api/v1/namespaces/kube-system/configmaps"
			}
			if a.Execute(context.Background(), task).ErrorCode != "cluster.agent_scope_denied" {
				t.Fatal("escape accepted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.issuance != 0 || f.workloads != 0 {
				t.Fatal("denied request reached Kubernetes")
			}
		})
	}
}
func TestManagedCredential401InvalidatesWithoutReplayingMutation(t *testing.T) {
	a, f := newCredentialFixture(t)
	f.workloadStatus = 401
	task := managedTask()
	task.Method = "POST"
	if result := a.Execute(context.Background(), task); result.Status != 401 {
		t.Fatal("401 response lost")
	}
	f.mu.Lock()
	if f.workloads != 1 || f.issuance != 1 {
		t.Error("mutation was retried")
	}
	f.workloadStatus = 0
	f.mu.Unlock()
	requireManagedSuccess(t, a, managedTask())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issuance != 2 {
		t.Fatal("401 token was not evicted")
	}
}
func TestManagedCredentialRefreshFailureDiscardsOldCredential(t *testing.T) {
	a, f := newCredentialFixture(t)
	requireManagedSuccess(t, a, managedTask())
	a.credentials.mu.Lock()
	for key, value := range a.credentials.entries {
		value.refresh = time.Now().Add(-time.Second)
		a.credentials.entries[key] = value
	}
	a.credentials.mu.Unlock()
	f.mu.Lock()
	f.issueStatus = 503
	f.mu.Unlock()
	if a.Execute(context.Background(), managedTask()).ErrorCode != "cluster.agent_credential_unavailable" {
		t.Fatal("refresh failure not surfaced")
	}
	a.credentials.mu.Lock()
	defer a.credentials.mu.Unlock()
	if len(a.credentials.entries) != 0 {
		t.Fatal("unusable token retained after refresh failure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.workloads != 1 {
		t.Fatal("refresh failure used stale token")
	}
}

func TestManagedCredentialAuditContainsMetadataWithoutTokens(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	a, f := newCredentialFixture(t)
	requireManagedSuccess(t, a, managedTask())
	a.credentials.clear()
	f.mu.Lock()
	f.issueStatus = 403
	f.mu.Unlock()
	result := a.Execute(context.Background(), managedTask())
	if result.ErrorCode != "cluster.agent_credential_unavailable" {
		t.Fatal("expected issuance failure")
	}
	audit := output.String()
	for _, sensitive := range []string{"issuer-secret", "deployment-secret", "never-expose-this-token"} {
		if strings.Contains(audit, sensitive) {
			t.Fatal("audit leaked credentials")
		}
	}
	for _, metadata := range []string{"workspace", "namespace", "serviceAccountUID", "requestedSeconds", "expiresAt", "request"} {
		if !strings.Contains(audit, metadata) {
			t.Fatalf("audit lacks %s metadata", metadata)
		}
	}
}

func TestManagedCredentialRevokedRunDiscardsCache(t *testing.T) {
	a, f := newCredentialFixture(t)
	requireManagedSuccess(t, a, managedTask())
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tasks") {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	defer central.Close()
	a.Config.ServerURL = central.URL
	a.HTTP = central.Client()
	if err := a.saveIdentity(agentprotocol.Identity{Token: "test-enrollment-identity", ClusterUID: "test-cluster", ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if a.Run(ctx) == nil {
		t.Fatal("revoked identity did not stop run")
	}
	a.credentials.mu.Lock()
	remaining := len(a.credentials.entries)
	a.credentials.mu.Unlock()
	if remaining != 0 {
		t.Fatal("revoked agent retained cached credentials")
	}
	if a.Execute(context.Background(), managedTask()).ErrorCode != "cluster.agent_credential_unavailable" {
		t.Fatal("stopped agent could use a deployment credential")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.workloads != 1 || f.issuance != 1 {
		t.Fatal("stopped agent minted or executed more workloads")
	}
}
