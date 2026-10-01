package kube

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/client-go/rest"
)

// ForWorkspaceBinding always includes the tenant and namespace in agent tasks.
func ForWorkspaceBinding(ctx context.Context, db *store.Store, key []byte, c store.Cluster, credential *string, clusterScope bool, workspace, namespace string) (*Clients, error) {
	a, err := db.ClusterAgent(ctx, c.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ForBinding(ctx, db, key, c, credential, clusterScope)
	}
	if err != nil {
		return nil, err
	}
	allowed, err := db.WorkspaceCanUseCluster(ctx, workspace, c.ID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("cluster is unavailable to this workspace")
	}
	profile := a.DefaultProfile
	if clusterScope {
		profile = a.ClusterProfile
		if profile == "" {
			return nil, errors.New("no cluster-scope agent profile configured")
		}
	}
	if a.Revoked || a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > time.Minute {
		return nil, &AgentError{Code: "cluster.agent_unavailable", Message: "cluster agent is offline; reconnect the agent and refresh the plan", Transient: true}
	}
	return clientsForConfig(&rest.Config{Host: "https://justcd-agent.invalid", Timeout: 30 * time.Second, Transport: &agentTransport{db: db, key: key, cluster: c.ID, workspace: workspace, namespace: namespace, profile: profile, clusterScope: clusterScope}})
}
func AgentEnabled(ctx context.Context, db *store.Store, id string) bool {
	_, err := db.ClusterAgent(ctx, id)
	return err == nil
}
func AgentClusterScope(ctx context.Context, db *store.Store, id string) bool {
	a, err := db.ClusterAgent(ctx, id)
	return err == nil && a.ClusterProfile != ""
}

type agentTransport struct {
	db                                     *store.Store
	key                                    []byte
	cluster, workspace, namespace, profile string
	clusterScope                           bool
}

func (t *agentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := r.Context().Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	task := agentprotocol.Request{ID: store.NewID(), WorkspaceID: t.workspace, Profile: t.profile, Namespace: t.namespace, ClusterScope: t.clusterScope, Method: r.Method, URI: r.URL.RequestURI(), ContentType: r.Header.Get("Content-Type"), Accept: r.Header.Get("Accept"), Deadline: deadline}
	if r.Body != nil {
		defer r.Body.Close()
		var err error
		task.Body, err = io.ReadAll(io.LimitReader(r.Body, agentprotocol.MaxBody+1))
		if err != nil {
			return nil, err
		}
		if len(task.Body) > agentprotocol.MaxBody {
			return nil, errors.New("Kubernetes request exceeds agent body limit")
		}
	}
	task.Execution, _ = r.Context().Value(executionKey{}).(*agentprotocol.Execution)
	plain, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	cipher, err := security.Encrypt(t.key, plain, "agent-task:"+task.ID+":request")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	if err = t.db.QueueAgentTask(ctx, task.ID, t.cluster, cipher, deadline); err != nil {
		return nil, &AgentError{Code: "cluster.agent_unavailable", Message: err.Error(), Transient: true}
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		t.db.DeleteAgentTask(cleanup, task.ID)
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := t.db.AgentTaskResult(ctx, task.ID)
		if err == nil {
			plain, err = security.Decrypt(t.key, data, "agent-task:"+task.ID+":response")
			if err != nil {
				return nil, err
			}
			var result agentprotocol.Result
			if err = json.Unmarshal(plain, &result); err != nil {
				return nil, err
			}
			if result.Error != "" {
				if result.ErrorCode == "cluster.agent_unknown_outcome" {
					return nil, uncertainAgentError(r, "cluster agent: "+result.Error)
				}
				return nil, &AgentError{Code: result.ErrorCode, Message: "cluster agent: " + result.Error}
			}
			return &http.Response{StatusCode: result.Status, Header: http.Header{"Content-Type": []string{result.ContentType}}, Body: io.NopCloser(bytes.NewReader(result.Body)), Request: r}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, uncertainAgentError(r, "cluster agent result could not be verified; refresh the plan before retrying")
		}
		select {
		case <-ctx.Done():
			return nil, uncertainAgentError(r, "cluster agent request timed out; execution outcome may be unknown, refresh the plan before retrying")
		case <-ticker.C:
		}
	}
}

type executionKey struct{}

func WithExecution(ctx context.Context, operationID, planID, digest string) context.Context {
	return context.WithValue(ctx, executionKey{}, &agentprotocol.Execution{OperationID: operationID, PlanID: planID, Digest: digest})
}

// AgentError prevents client-go's URL wrappers from turning an uncertain write
// into a generic retryable network error.
type AgentError struct {
	Code, Message string
	Transient     bool
}

func (e *AgentError) Error() string { return e.Message }
func (e *AgentError) ErrorCode() string {
	if e.Code == "" {
		return "cluster.agent_request_failed"
	}
	return e.Code
}
func (e *AgentError) Retryable() bool { return e.Transient }
func uncertainAgentError(r *http.Request, message string) error {
	readOnly := r.Method == "GET" || r.URL.Query().Get("dryRun") == "All" || strings.HasSuffix(r.URL.Path, "/selfsubjectaccessreviews")
	if readOnly {
		return &AgentError{Code: "cluster.agent_unavailable", Message: message, Transient: true}
	}
	return &AgentError{Code: "cluster.agent_unknown_outcome", Message: message}
}
