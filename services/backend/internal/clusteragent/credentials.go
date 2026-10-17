package clusteragent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"golang.org/x/sync/singleflight"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Cache lifetime is limited by the granted expiry, not the requested lifetime.
// It is local to one Agent/cluster, never persisted or returned over the wire.
type deploymentCredentialKey struct{ profile, workspace, namespace, uid, audience string }
type deploymentCredential struct {
	key              deploymentCredentialKey
	token            string
	expires, refresh time.Time
}
type deploymentCredentialCache struct {
	mu      sync.Mutex
	entries map[deploymentCredentialKey]deploymentCredential
	pending singleflight.Group
	stopped bool
}

func (c *deploymentCredentialCache) load(key deploymentCredentialKey) (deploymentCredential, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.entries {
		if !v.refresh.After(now) {
			delete(c.entries, k)
		}
	}
	value, ok := c.entries[key]
	return value, ok
}
func (c *deploymentCredentialCache) save(value deploymentCredential) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return false
	}
	if c.entries == nil {
		c.entries = make(map[deploymentCredentialKey]deploymentCredential)
	}
	// Bound memory even under namespace/account recreation or operator misconfiguration.
	if len(c.entries) >= 256 {
		clear(c.entries)
	}
	c.entries[value.key] = value
	return true
}
func (c *deploymentCredentialCache) invalidate(value deploymentCredential) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[value.key].token == value.token {
		delete(c.entries, value.key)
	}
}
func (c *deploymentCredentialCache) clear() { c.mu.Lock(); defer c.mu.Unlock(); clear(c.entries) }
func (c *deploymentCredentialCache) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	clear(c.entries)
}
func (c *deploymentCredentialCache) active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.stopped
}

func (a *Agent) deploymentCredential(ctx context.Context, task agentprotocol.Request, profile agentprotocol.Profile) (deploymentCredential, error) {
	if !a.credentials.active() {
		return deploymentCredential{}, errors.New("agent credential service has stopped; reconnect the agent")
	}
	// Validate again at the issuer boundary. The issuer endpoint and account are
	// constructed locally and cannot be supplied by a task.
	if err := agentprotocol.Validate(task, profile); err != nil {
		return deploymentCredential{}, err
	}
	base := "/api/v1/namespaces/" + task.Namespace + "/serviceaccounts/" + agentprotocol.DeploymentServiceAccount
	var sa corev1.ServiceAccount
	if err := a.credentialRequest(ctx, profile.Name, http.MethodGet, base, nil, &sa); err != nil {
		return deploymentCredential{}, errors.New("deployment service account unavailable; prepare the namespace and named-account issuer RBAC, then retry")
	}
	if sa.UID == "" || sa.DeletionTimestamp != nil || sa.Name != agentprotocol.DeploymentServiceAccount || sa.Namespace != task.Namespace {
		return deploymentCredential{}, errors.New("deployment service account identity is invalid or deleting")
	}
	if profile.DeploymentCredentials.Provisioning != nil {
		environment, ownershipErr := a.readEnvironment(task.Namespace)
		if ownershipErr != nil {
			return deploymentCredential{}, ownershipErr
		}
		if environment != nil && environment.Managed && environment.ServiceAccountUID != string(sa.UID) {
			return deploymentCredential{}, errors.New("managed deployment account UID changed; refusing credential issuance")
		}
	}
	policy := profile.DeploymentCredentials
	key := deploymentCredentialKey{profile.Name, task.WorkspaceID, task.Namespace, string(sa.UID), policy.Audience}
	if value, ok := a.credentials.load(key); ok {
		return value, nil
	}
	// JSON is an unambiguous key even if operator-configured strings contain separators.
	encoded, _ := json.Marshal(keyToStrings(key))
	result := a.credentials.pending.DoChan(string(encoded), func() (any, error) {
		if value, ok := a.credentials.load(key); ok {
			return value, nil
		}
		request := authenticationv1.TokenRequest{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenRequest"}, Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &policy.LifetimeSeconds}}
		if policy.Audience != "" {
			request.Spec.Audiences = []string{policy.Audience}
		}
		var response authenticationv1.TokenRequest
		if err := a.credentialRequest(ctx, profile.Name, http.MethodPost, base+"/token", request, &response); err != nil {
			slog.Warn("deployment token issuance failed", "profile", profile.Name, "workspace", task.WorkspaceID, "namespace", task.Namespace, "request", task.ID)
			return nil, errors.New("deployment token issuance failed; check named-account TokenRequest permission and API availability, then retry")
		}
		// Re-read after issuance so an account replaced between GET and TokenRequest
		// cannot poison a cache entry for the previous UID.
		var current corev1.ServiceAccount
		if err := a.credentialRequest(ctx, profile.Name, http.MethodGet, base, nil, &current); err != nil || current.UID != sa.UID || current.DeletionTimestamp != nil {
			return nil, errors.New("deployment service account changed during issuance; retry after preparation completes")
		}
		now := time.Now()
		expires := response.Status.ExpirationTimestamp.Time
		remaining := expires.Sub(now)
		if response.Status.Token == "" || remaining <= 30*time.Second {
			return nil, errors.New("token helper returned an empty or expired deployment credential")
		}
		// Leave enough validity for the agent's 25-second HTTP timeout.
		margin := 30 * time.Second
		value := deploymentCredential{key: key, token: response.Status.Token, expires: expires, refresh: expires.Add(-margin)}
		// A canceled task may return before this shared refresh finishes. Do not
		// let it repopulate the cache after Run exits or enrollment is rejected.
		if !a.credentials.save(value) {
			return nil, errors.New("agent stopped during deployment credential issuance")
		}
		operation := ""
		if task.Execution != nil {
			operation = task.Execution.OperationID
		}
		slog.Info("deployment token issued", "profile", profile.Name, "workspace", task.WorkspaceID, "namespace", task.Namespace, "serviceAccount", agentprotocol.DeploymentServiceAccount, "serviceAccountUID", sa.UID, "requestedSeconds", policy.LifetimeSeconds, "expiresAt", expires, "request", task.ID, "operation", operation)
		return value, nil
	})
	select {
	case <-ctx.Done():
		return deploymentCredential{}, errors.New("deployment token request deadline exceeded")
	case result := <-result:
		if result.Err != nil {
			return deploymentCredential{}, result.Err
		}
		value := result.Val.(deploymentCredential)
		if !value.expires.After(time.Now().Add(30 * time.Second)) {
			a.credentials.invalidate(value)
			return deploymentCredential{}, errors.New("deployment credential expired before execution")
		}
		return value, nil
	}
}
func keyToStrings(k deploymentCredentialKey) []string {
	return []string{k.profile, k.workspace, k.namespace, k.uid, k.audience}
}

// Never propagate upstream bodies, request dumps, or decoder errors: a malformed
// TokenRequest response can contain a token even on a non-success status.
func (a *Agent) credentialRequest(ctx context.Context, profile, method, path string, body, out any) error {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.KubernetesURL+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid credential request")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.Local[profile].Do(req)
	if err != nil {
		return errors.New("credential service unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("credential service returned HTTP %d", res.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("invalid credential response")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("invalid credential response")
	}
	return nil
}
