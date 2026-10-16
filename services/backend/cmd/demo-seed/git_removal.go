package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

const gitRemovalDemoName = "Demo Git Removal · Awaiting deletion"

// seedGitRemoval is additive so existing demo workspaces gain this use case.
// Discovery stays disabled: the .invalid source represents a removed definition.
func seedGitRemoval(ctx context.Context, tx *sql.Tx, workspaceID string) (bool, error) {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE workspace_id=$1 AND name=$2`, workspaceID, gitRemovalDemoName).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var clusterID, sourceID, credentialID, ownerID string
	if err := tx.QueryRowContext(ctx, `SELECT a.cluster_id,a.source_id,c.default_credential_id FROM applications a JOIN clusters c ON c.id=a.cluster_id WHERE a.workspace_id=$1 AND c.api_server='https://kubernetes.justcd-demo.invalid' ORDER BY a.created_at LIMIT 1`, workspaceID).Scan(&clusterID, &sourceID, &credentialID); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM workspace_memberships WHERE workspace_id=$1 AND role='owner' LIMIT 1`, workspaceID).Scan(&ownerID); err != nil {
		return false, err
	}
	appID, repositoryID := store.NewID(), store.NewID()
	const namespace = "demo-retired-app"
	const revision = "6666666666666666666666666666666666666666"
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, store.NewID(), workspaceID, clusterID, namespace); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO repository_configurations(id,workspace_id,source_id,revision,enabled,last_commit,last_checked_at) VALUES($1,$2,$3,'demo/removed-application',FALSE,$4,$5) ON CONFLICT(workspace_id,source_id,revision) DO UPDATE SET revision=EXCLUDED.revision RETURNING id`, repositoryID, workspaceID, sourceID, revision, now).Scan(&repositoryID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,last_synced_revision,health,decommissioning,repository_configuration_id,configuration_path,configuration_commit,configuration_missing) VALUES($1,$2,$3,$4,'demo/removed-application','apps/retired-app','yaml',$5,$6,'auto-safe',86400,$7,$8,'deletion_pending',TRUE,$9,'apps/retired-app/justcd.yaml',$8,TRUE)`, appID, workspaceID, gitRemovalDemoName, sourceID, clusterID, jsonBytes([]store.NamespaceBinding{{Namespace: namespace}}), now, revision, repositoryID); err != nil {
		return false, err
	}
	bindings := []core.Binding{{ClusterID: clusterID, Namespace: namespace, CredentialRef: credentialID}, {ClusterID: clusterID, ClusterScope: true, CredentialRef: credentialID}}
	live, plan := gitRemovalPlan(appID, clusterID, bindings)
	old, err := core.BuildPlan(appID, revision, bindings, live, live)
	if err != nil {
		return false, err
	}
	oldID := store.NewID()
	if err := insertPlan(ctx, tx, oldID, old, live, ownerID, "applied", now.Add(-2*time.Hour), now.Add(-105*time.Minute)); err != nil {
		return false, err
	}
	if err := insertOperation(ctx, tx, appID, &oldID, &ownerID, "succeeded", "Demo history: deployed before apps/retired-app, including justcd.yaml, was removed from Git.", now.Add(-2*time.Hour), now.Add(-119*time.Minute)); err != nil {
		return false, err
	}
	planID := store.NewID()
	if err := insertPlan(ctx, tx, planID, plan, []core.Resource{}, ownerID, "current", now, now.Add(24*time.Hour)); err != nil {
		return false, err
	}
	for _, r := range live {
		if err := insertManagedResource(ctx, tx, appID, r); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'application.decommission_plan_created','application',$2,$3)`, ownerID, appID, jsonBytes(map[string]any{"demo": true, "planId": planID, "reason": "Application directory and justcd.yaml removed from Git", "namespacePolicy": "Explicitly app-owned Namespace included; shared namespaces are preserved"})); err != nil {
		return false, err
	}
	return true, nil
}

func gitRemovalPlan(appID, clusterID string, bindings []core.Binding) ([]core.Resource, core.Plan) {
	const ns = "demo-retired-app"
	live := []core.Resource{
		resource(clusterID, ns, "Deployment", "retired-web", "retired-web-v1", "demo-retired-web-uid", "12", map[string]any{"spec": map[string]any{"replicas": 2, "selector": map[string]any{"matchLabels": map[string]string{"app": "retired-web"}}, "template": map[string]any{"metadata": map[string]any{"labels": map[string]string{"app": "retired-web"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "web", "image": "nginx:stable"}}}}}}),
		resource(clusterID, ns, "Service", "retired-web", "retired-service-v1", "demo-retired-service-uid", "7", map[string]any{"spec": map[string]any{"selector": map[string]string{"app": "retired-web"}, "ports": []any{map[string]any{"port": 80, "targetPort": 80}}}}),
		resource(clusterID, ns, "ConfigMap", "retired-settings", "retired-config-v1", "demo-retired-config-uid", "3", map[string]any{"data": map[string]string{"DEMO": "Removed from Git; awaiting reviewed deletion"}}),
		resource(clusterID, "", "Namespace", ns, "retired-namespace-v1", "demo-retired-namespace-uid", "5", nil),
	}
	for i := range live {
		live[i].Identity.ClusterScoped = live[i].Identity.Namespace == ""
		live[i] = addApplicationOwnership(live[i], appID)
	}
	plan, err := core.BuildPlan(appID, "6666666666666666666666666666666666666666", bindings, nil, live)
	if err != nil {
		panic(err)
	}
	plan.Decommission = true
	plan.ApprovalKind = "deletion"
	plan.RequiredApprovals = 1
	plan.RequiresApproval = true
	plan.ApproverRoles = []string{"owner"}
	plan.ApproverUserIDs = []string{}
	identities := make([]core.Identity, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		identities = append(identities, change.Identity)
	}
	plan.ApprovalReasons = []core.ApprovalReason{{Code: "deletion", Rule: "workspace.deletion", Description: "The application directory and justcd.yaml were removed from Git. Review all managed resource deletions, including the explicitly owned Namespace, before applying.", Resources: identities}}
	if err := core.RefreshDigest(&plan); err != nil {
		panic(err)
	}
	return live, plan
}
