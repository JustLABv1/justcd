//go:build integration

package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testHelmRegistrySources(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	workspace := "integration-workspace"
	credential := Credential{ID: "helm-registry-credential", WorkspaceID: &workspace, Name: "Registry", Kind: "helm-registry", Endpoint: "oci://registry.example.com/charts", Cipher: []byte{1}}
	if err := s.CreateCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour)
	if err := s.CreateCredential(ctx, Credential{ID: "helm-registry-expired", WorkspaceID: &workspace, Name: "Expired", Kind: "helm-registry", Endpoint: "oci://old.example.com", Cipher: []byte{1}, ExpiresAt: &expired}); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.CredentialByID(ctx, credential.ID)
	if err != nil || loaded.Endpoint != credential.Endpoint {
		t.Fatalf("credential endpoint = %q, err = %v", loaded.Endpoint, err)
	}
	loaded.Endpoint = "https://charts.example.com/stable"
	if err := s.UpdateCredential(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	registries, err := s.HelmRegistryCredentials(ctx, workspace)
	if err != nil || len(registries) != 1 || registries[0].ID != credential.ID || registries[0].Endpoint != "https://charts.example.com/stable" {
		t.Fatalf("registry credentials = %+v, err = %v", registries, err)
	}
	if other, err := s.HelmRegistryCredentials(ctx, "another-workspace"); err != nil || len(other) != 0 {
		t.Fatalf("other workspace sees registry credentials: %+v, %v", other, err)
	}

	chart := &core.HelmChartSource{Repository: "oci://registry.example.com/charts", Chart: "podinfo", Version: "6.5.0"}
	app := Application{ID: "helm-registry-app", WorkspaceID: workspace, Name: "registry-chart", SourceID: "integration-git", Revision: "main", ManifestPath: ".", Renderer: "helm", HelmChart: chart, ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{{Namespace: "default"}}, SyncPolicy: "manual", PollSeconds: 300, RetryPolicy: DefaultRetryPolicy(), Labels: map[string]string{}}
	if err := s.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	stored, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || !reflect.DeepEqual(stored.HelmChart, chart) {
		t.Fatalf("stored chart = %+v, err = %v", stored.HelmChart, err)
	}
	stored.HelmChart.Version = "6.6.0"
	if err := s.UpdateApplication(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if updated, err := s.ApplicationByID(ctx, app.ID); err != nil || updated.HelmChart == nil || updated.HelmChart.Version != "6.6.0" {
		t.Fatalf("updated chart = %+v, err = %v", updated.HelmChart, err)
	}

	settings := core.RollbackSettings{SourceID: "integration-git", Revision: "v1", ManifestPath: "charts/podinfo", Renderer: "helm", ClusterID: "integration-cluster", Namespaces: []string{"default"}}
	if err := s.PinApplicationForRollback(ctx, app.ID, "helm-registry-plan", settings, "", false); err != nil {
		t.Fatal(err)
	}
	pinned, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || pinned.HelmChart != nil || pinned.RollbackResumeState == nil || pinned.RollbackResumeState.HelmChart == nil || pinned.RollbackResumeState.HelmChart.Version != "6.6.0" {
		t.Fatalf("rollback pin did not swap chart source: %+v, %v", pinned, err)
	}
	if err := s.ResumeRollbackTracking(ctx, app.ID, ""); err != nil {
		t.Fatal(err)
	}
	if resumed, err := s.ApplicationByID(ctx, app.ID); err != nil || resumed.HelmChart == nil || resumed.HelmChart.Version != "6.6.0" {
		t.Fatalf("resumed chart = %+v, err = %v", resumed.HelmChart, err)
	}

	group := ApplicationGroup{ID: "helm-registry-group", WorkspaceID: workspace, Name: "registry-group", SourceID: "integration-git", Revision: "main", ManifestPath: ".", Renderer: "helm", HelmValuesFiles: []string{}, HelmChart: chart, SyncPolicy: "manual", PollSeconds: 300}
	member := Application{ID: "helm-registry-group-app", Name: "registry-group-app", ClusterID: "integration-cluster", Namespaces: []NamespaceBinding{{Namespace: "default"}}}
	if err := s.CreateApplicationGroup(ctx, group, []Application{member}); err != nil {
		t.Fatal(err)
	}
	storedGroup, err := s.ApplicationGroupByID(ctx, group.ID)
	if err != nil || !reflect.DeepEqual(storedGroup.HelmChart, chart) {
		t.Fatalf("group chart = %+v, err = %v", storedGroup.HelmChart, err)
	}
	storedGroup.HelmChart = nil
	storedGroup.ManifestPath = "charts/podinfo"
	if err := s.UpdateApplicationGroup(ctx, storedGroup); err != nil {
		t.Fatal(err)
	}
	members, err := s.ApplicationsByGroupID(ctx, group.ID)
	if err != nil || len(members) != 1 || members[0].HelmChart != nil || members[0].ManifestPath != "charts/podinfo" {
		t.Fatalf("group update did not reach targets: %+v, %v", members, err)
	}
}
