package kube

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func managedProvisioningProfile(ctx context.Context, db *store.Store, cluster, workspace string) (*agentprotocol.Profile, error) {
	agent, err := db.ClusterAgent(ctx, cluster)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, p := range agent.Profiles {
		if p.Name == agent.DefaultProfile && p.DeploymentCredentials != nil && p.DeploymentCredentials.Provisioning != nil {
			if len(p.WorkspaceIDs) != 1 || p.WorkspaceIDs[0] != workspace {
				return nil, errors.New("workspace is not authorized for namespace provisioning")
			}
			if agent.Revoked || agent.LastSeenAt == nil || time.Since(*agent.LastSeenAt) > time.Minute {
				return nil, errors.New("namespace provisioner is offline or revoked")
			}
			return &p, nil
		}
	}
	return nil, nil
}
func manageEnvironment(ctx context.Context, db *store.Store, key []byte, app store.Application, namespace string, p agentprotocol.Profile, e agentprotocol.EnvironmentRequest) (*agentprotocol.EnvironmentStatus, error) {
	allowed, err := db.WorkspaceCanUseCluster(ctx, app.WorkspaceID, app.ClusterID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("workspace cannot use this cluster")
	}
	transport := &agentTransport{ctx: ctx, db: db, key: key, cluster: app.ClusterID, workspace: app.WorkspaceID, namespace: namespace, profile: p.Name, environment: &e}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://justcd-agent.invalid/justcd/environments", nil)
	if err != nil {
		return nil, err
	}
	res, err := transport.RoundTrip(req)
	if err != nil {
		var failure *AgentError
		if errors.As(err, &failure) && failure.Environment != nil {
			status := failure.Environment
			status.Reason = failure.Message
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if saveErr := db.SaveManagedEnvironmentStatus(persistCtx, e.ID, *status); saveErr != nil {
				return status, saveErr
			}
			return status, err
		}
		status := &agentprotocol.EnvironmentStatus{NamespaceUID: e.ExpectedUID, Phase: "failed", FailedPhase: e.Action, Reason: "Environment lifecycle request failed; retry after checking agent connectivity and ownership."}
		if errors.As(err, &failure) {
			status.Reason = failure.Message
		}
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if saveErr := db.SaveManagedEnvironmentStatus(persistCtx, e.ID, *status); saveErr != nil {
			return status, saveErr
		}
		_ = db.Audit(persistCtx, "", "namespace.environment.failed", "application", app.ID, map[string]any{"environmentId": e.ID, "namespace": namespace, "action": e.Action, "phase": status.Phase, "reason": status.Reason})
		return status, err
	}
	defer res.Body.Close()
	var status agentprotocol.EnvironmentStatus
	if json.NewDecoder(res.Body).Decode(&status) != nil {
		return nil, errors.New("invalid environment lifecycle response")
	}
	if err = db.SaveManagedEnvironmentStatus(ctx, e.ID, status); err != nil {
		return &status, err
	}
	_ = db.Audit(ctx, "", "namespace.environment."+e.Action, "application", app.ID, map[string]any{"workspaceId": app.WorkspaceID, "clusterId": app.ClusterID, "namespace": namespace, "environmentId": e.ID, "namespaceUid": status.NamespaceUID, "phase": status.Phase, "shared": status.Shared, "reason": status.Reason})
	return &status, nil
}

// EnsureApplicationNamespace executes the bounded provisioning operation before
// any discovery/read/apply using the namespace deployment identity.
func EnsureApplicationNamespace(ctx context.Context, db *store.Store, key []byte, app store.Application, namespace string) (bool, error) {
	p, err := managedProvisioningProfile(ctx, db, app.ClusterID, app.WorkspaceID)
	if err != nil || p == nil {
		return false, err
	}
	if err = validateEnvironmentRoute(*p, app, namespace, "ensure", "validation"); err != nil {
		return true, err
	}
	if _, err = db.NamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, namespace); err != nil {
		return true, errors.New("namespace is not bound to this workspace")
	}
	e, err := db.RegisterManagedEnvironment(ctx, app, namespace)
	if err != nil {
		return true, err
	}
	status, err := manageEnvironment(ctx, db, key, app, namespace, *p, agentprotocol.EnvironmentRequest{ID: e.ID, ApplicationID: app.ID, Action: "ensure", CreateNamespace: app.CreateNamespaces && !app.Decommissioning, Shared: e.Shared, ExpectedUID: e.NamespaceUID})
	if err == nil && (status == nil || status.Phase != "ready") {
		return true, errors.New("namespace environment provisioning did not reach the ready phase")
	}
	return true, err
}

// CleanupApplicationNamespaces is called only after workload cleanup, never by
// orphan/keep deletion. Local policy and both ownership records govern removal.
func CleanupApplicationNamespaces(ctx context.Context, db *store.Store, key []byte, app store.Application, preserve bool) error {
	environments, err := db.ManagedEnvironmentsOwnedByApplication(ctx, app.ID, app.WorkspaceID)
	if err != nil {
		return err
	}
	for _, existing := range environments {
		if existing.Phase == "deleted" {
			continue
		}
		destination := app
		destination.ClusterID = existing.ClusterID
		p, err := managedProvisioningProfile(ctx, db, existing.ClusterID, app.WorkspaceID)
		if err != nil {
			return err
		}
		if p == nil {
			if existing.Phase == "deleting" {
				return errors.New("namespace cleanup is pending; restore its managed agent profile before removing the application")
			}
			// Changing to a static connection never grants its credentials cleanup rights.
			if err = db.SaveManagedEnvironmentStatus(ctx, existing.ID, agentprotocol.EnvironmentStatus{NamespaceUID: existing.NamespaceUID, Phase: "retained", Shared: true, Reason: "namespace retained because its provisioning profile is no longer configured"}); err != nil {
				return err
			}
			continue
		}
		if err = validateEnvironmentRoute(*p, destination, existing.Namespace, "release", existing.ID); err != nil {
			return err
		}
		e, keep, err := db.BeginManagedEnvironmentDeletion(ctx, destination, existing.Namespace, preserve || !p.DeploymentCredentials.Provisioning.DeleteNamespaces)
		if err != nil {
			return err
		}
		if e.ID == "" {
			continue
		}
		status, err := manageEnvironment(ctx, db, key, destination, existing.Namespace, *p, agentprotocol.EnvironmentRequest{ID: e.ID, ApplicationID: app.ID, Action: "release", ExpectedUID: e.NamespaceUID, Shared: e.Shared, PreserveNamespace: keep})
		if err != nil {
			return err
		}
		if status.Phase == "deleting" {
			return errors.New("namespace cleanup is waiting for finalizers; retry application deletion after the namespace disappears")
		}
		if status.Phase != "deleted" && status.Phase != "retained" {
			return errors.New("namespace cleanup did not complete; retry after resolving the reported phase")
		}
	}
	return nil
}

func AgentNamespaceProvisioning(ctx context.Context, db *store.Store, cluster, workspace string) (bool, error) {
	p, err := managedProvisioningProfile(ctx, db, cluster, workspace)
	return p != nil, err
}

func validateEnvironmentRoute(profile agentprotocol.Profile, app store.Application, namespace, action, id string) error {
	return agentprotocol.Validate(agentprotocol.Request{Profile: profile.Name, WorkspaceID: app.WorkspaceID, Namespace: namespace, Method: "POST", URI: "/justcd/environments", Deadline: time.Now().Add(30 * time.Second), Environment: &agentprotocol.EnvironmentRequest{ID: id, ApplicationID: app.ID, Action: action}}, profile)
}
