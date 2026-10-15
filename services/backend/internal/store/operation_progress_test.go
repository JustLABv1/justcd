package store

import (
	"context"
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/core"
	"reflect"
	"testing"
	"time"
)

func TestOperationProgressSerializesMissingCompletedAsArray(t *testing.T) {
	for _, raw := range []string{`{}`, `{"completed":null}`, `{"completed":[]}`} {
		var p OperationProgress
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if string(result["completed"]) != "[]" || string(result["resources"]) != "[]" {
			t.Fatalf("%s returned %s", raw, encoded)
		}
	}
}

func TestResourceProgressPartialFailurePreservesAppliedResults(t *testing.T) {
	identities := []core.Identity{{Kind: "ConfigMap", Name: "applied"}, {Kind: "ConfigMap", Name: "uncertain"}, {Kind: "ConfigMap", Name: "unattempted"}}
	plan := core.Plan{Changes: []core.Change{{Identity: identities[0], Kind: core.Create}, {Identity: identities[1], Kind: core.Update}, {Identity: identities[2], Kind: core.Delete}}}
	p := mustOperationProgress(t, plan)
	p.Phase = "applying"
	p.BeginResource(identities[0], 2)
	p.CompleteResource(identities[0])
	p.BeginResource(identities[1], 2)
	if p.Resources[1].Status != "retrying" {
		t.Fatal("later attempt was not reported as retrying")
	}
	p.Stop("The response was lost; review cluster state")
	if p.Total != 3 || len(p.Completed) != 1 || p.Current != nil {
		t.Fatalf("lost progress: %+v", p)
	}
	for i, want := range []string{"succeeded", "failed", "skipped"} {
		if p.Resources[i].Status != want {
			t.Fatalf("resource %d: %+v", i, p.Resources[i])
		}
	}
	if p.Resources[1].AttemptCount != 2 || p.Resources[1].StartedAt == nil || p.Resources[1].FinishedAt == nil || p.Resources[2].StartedAt != nil {
		t.Fatalf("incorrect attempt timestamps: %+v", p.Resources)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var resumed OperationProgress
	if err := json.Unmarshal(raw, &resumed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, resumed) {
		t.Fatalf("progress changed after restart: %+v", resumed)
	}
}

func TestResourceProgressIncludesNamespacePrerequisites(t *testing.T) {
	namespace := core.Identity{APIVersion: "v1", Kind: "Namespace", Name: "apps"}
	p := mustOperationProgress(t, core.Plan{NamespaceCreations: []core.Identity{namespace}, Changes: []core.Change{{Identity: namespace, Kind: core.Create}, {Identity: core.Identity{Kind: "Deployment", Name: "api"}, Kind: core.Create}}})
	if p.Total != 2 || p.Resources[0].Action != "create_namespace" {
		t.Fatalf("incorrect resource count: %+v", p)
	}
}

func TestTerminalProgressAfterDisconnect(t *testing.T) {
	p := mustOperationProgress(t, core.Plan{Changes: []core.Change{{Identity: core.Identity{Kind: "ConfigMap", Name: "in-flight"}, Kind: core.Create}, {Identity: core.Identity{Kind: "ConfigMap", Name: "queued"}, Kind: core.Create}}})
	p.BeginResource(p.Resources[0].Identity, 1)
	finished := time.Now().UTC()
	op := Operation{Status: "failed", Message: "Worker lost; resource outcome requires review", Progress: p, FinishedAt: &finished}
	op.NormalizeProgress()
	if op.Progress.Resources[0].Status != "failed" || op.Progress.Resources[1].Status != "skipped" {
		t.Fatalf("uncertain mutation reported as unattempted: %+v", op)
	}
	if *op.Progress.Resources[0].FinishedAt != finished {
		t.Fatal("recovery time changed during read")
	}
}

func TestDeploymentCannotSucceedWithUnfinishedResourceResults(t *testing.T) {
	for _, status := range []string{"queued", "running", "retrying", "failed", "skipped"} {
		op := Operation{Progress: OperationProgress{Resources: []ResourceProgress{{Status: status}}}}
		if err := (&Store{}).CompleteDeployment(context.Background(), op, "", "synced", "sync.succeeded", nil); err == nil {
			t.Fatalf("operation succeeded with %s resource", status)
		}
	}
}

func mustOperationProgress(t *testing.T, plan core.Plan) OperationProgress {
	t.Helper()
	progress, err := NewOperationProgress(plan)
	if err != nil {
		t.Fatal(err)
	}
	return progress
}

func TestProgressMatchesDependencyExecutionOrder(t *testing.T) {
	deployment := core.Change{Kind: core.Create, Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "apps", Name: "api"}, After: json.RawMessage(`{"spec":{"template":{"spec":{"serviceAccountName":"runner"}}}}`)}
	account := core.Change{Kind: core.Create, Identity: core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "ServiceAccount", Namespace: "apps", Name: "runner"}, After: json.RawMessage(`{}`)}
	deleted := core.Change{Kind: core.Delete, Identity: core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "ConfigMap", Namespace: "apps", Name: "obsolete"}}
	namespace := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Namespace", Name: "apps", ClusterScoped: true}
	plan := core.Plan{NamespaceCreations: []core.Identity{namespace}, Changes: []core.Change{deployment, deleted, account}}
	p := mustOperationProgress(t, plan)
	for i, identity := range []core.Identity{namespace, account.Identity, deployment.Identity, deleted.Identity} {
		if p.Resources[i].Identity != identity || p.Resources[i].Status != "queued" {
			t.Fatalf("wrong timeline order at %d: %+v", i, p.Resources)
		}
		p.BeginResource(identity, 1)
		p.CompleteResource(identity)
	}
	if p.Total != 4 || len(p.Completed) != 4 {
		t.Fatalf("lost execution results: %+v", p)
	}
}

func TestProgressRejectsCyclicPlan(t *testing.T) {
	change := func(name, owner string) core.Change {
		raw, _ := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": []any{map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "name": owner}}}})
		return core.Change{Kind: core.Create, Identity: core.Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "apps", Name: name}, After: raw}
	}
	if _, err := NewOperationProgress(core.Plan{Changes: []core.Change{change("a", "b"), change("b", "a")}}); err == nil {
		t.Fatal("unsafe plan produced queued progress")
	}
}
