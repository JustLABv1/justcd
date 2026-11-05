package core

import (
	"encoding/json"
	"testing"
)

func TestRetainedServicesAndNamespaceCannotBeSelectedForDeletion(t *testing.T) {
	db := Change{Kind: Delete, Identity: Identity{ClusterID: "c", APIVersion: "stackgres.io/v1", Kind: "SGCluster", Namespace: "apps", Name: "db"}, Before: json.RawMessage(`{"metadata":{"annotations":{"justcd.io/retain":"true"}}}`)}
	ns := Change{Kind: Delete, Identity: Identity{ClusterID: "c", APIVersion: "v1", Kind: "Namespace", Name: "apps", ClusterScoped: true}}
	app := Change{Kind: Delete, Identity: Identity{ClusterID: "c", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "apps", Name: "app"}}
	plan := Plan{Changes: []Change{db, ns, app}}
	RetainResources(&plan)
	if len(plan.Changes) != 1 || plan.Changes[0].Identity.Kind != "Deployment" || len(plan.Ignored) != 2 || !plan.Selection.KeepNamespaces {
		t.Fatal(plan)
	}
	// Re-including exclusions through another plan selection must not bypass retention.
	plan.Changes = append(plan.Changes, plan.Ignored...)
	plan.Ignored = nil
	RetainResources(&plan)
	if len(plan.Changes) != 1 || len(plan.Ignored) != 2 {
		t.Fatal(plan)
	}
}
func TestOperatorServiceDependencyOrder(t *testing.T) {
	vault := rolloutChange("secrets.hashicorp.com/v1beta1", "VaultStaticSecret", "apps", "credentials", `{"spec":{"destination":{"name":"database"}}}`)
	script := rolloutChange("stackgres.io/v1", "SGScript", "apps", "init", `{"spec":{"scripts":[{"scriptFrom":{"secretKeyRef":{"name":"database","key":"init.sql"}}}]}}`)
	db := rolloutChange("stackgres.io/v1", "SGCluster", "apps", "db", `{"spec":{"managedSql":{"scripts":[{"sgScript":"init"}]}}}`)
	ordered, err := OrderChanges([]Change{db, script, vault})
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Identity.Kind != "VaultStaticSecret" || ordered[1].Identity.Kind != "SGScript" || ordered[2].Identity.Kind != "SGCluster" {
		t.Fatal(ordered)
	}
}
