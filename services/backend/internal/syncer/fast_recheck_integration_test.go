//go:build integration

package syncer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// verifyIntegrationFastRecheck runs against the delivery fixture after its
// worker stopped: plans are applied with executeCrashFixture.
func verifyIntegrationFastRecheck(t *testing.T, ctx context.Context, svc *Service, kubeClient *kubernetes.Clientset, repo *integrationGitRepo, workspaceID, clusterID, sourceID, credentialID, ownerID, namespace string) {
	t.Run("fast-recheck", func(t *testing.T) {
		db := svc.Store
		id := store.NewID()
		app := store.Application{ID: id, WorkspaceID: workspaceID, Name: "Fast recheck " + id, SourceID: sourceID, Revision: "main", ManifestPath: "fast-recheck/" + id, Renderer: "yaml", ClusterID: clusterID, Namespaces: []store.NamespaceBinding{{Namespace: namespace, CredentialID: &credentialID}}, SyncPolicy: "manual", PollSeconds: 300}
		if err := db.CreateApplication(ctx, app); err != nil {
			t.Fatal(err)
		}
		name := func(suffix string) string { return fmt.Sprintf("fast-%s-%s", id[:8], suffix) }
		write := func(value string) {
			for _, suffix := range []string{"a", "b", "c"} {
				repo.write(t, app.ManifestPath+"/"+suffix+".yaml", fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  mode: %s\n", name(suffix), namespace, value))
			}
			repo.commit(t, "fast recheck "+id+" "+value)
		}
		plan := func() store.PlanRecord {
			t.Helper()
			record, err := svc.BuildPlan(ctx, app.ID, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			if record.RecheckBasis == "" {
				t.Fatal("plan recorded no recheck basis")
			}
			return record
		}
		expectFast := func(record store.PlanRecord) {
			t.Helper()
			current, err := db.ApplicationByID(ctx, app.ID)
			if err != nil {
				t.Fatal(err)
			}
			fresh, live, err := svc.fastRecheck(ctx, current, record)
			if err != nil {
				t.Fatalf("unchanged plan was not rechecked quickly: %v", err)
			}
			if err := preserveReviewSchema(&fresh, record.Plan); err != nil {
				t.Fatal(err)
			}
			if fresh.Digest != record.Plan.Digest || live == nil {
				t.Fatalf("fast recheck changed the reviewed plan: %s != %s", fresh.Digest, record.Plan.Digest)
			}
			full, err := svc.calculatePlan(ctx, current, record.Plan.Selection)
			if err != nil {
				t.Fatal(err)
			}
			if err := preserveReviewSchema(&full.plan, record.Plan); err != nil {
				t.Fatal(err)
			}
			if full.plan.Digest != fresh.Digest {
				t.Fatalf("fast and full rechecks disagree: %s != %s", fresh.Digest, full.plan.Digest)
			}
		}
		expectFull := func(record store.PlanRecord, reason string) {
			t.Helper()
			current, err := db.ApplicationByID(ctx, app.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = svc.fastRecheck(ctx, current, record)
			var full errFullRecheck
			if !errors.As(err, &full) || full.reason != reason {
				t.Fatalf("expected a full recheck because %q, got %v", reason, err)
			}
			fresh, err := svc.RecheckPlan(ctx, current, record)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Digest == record.Plan.Digest {
				t.Fatal("the full recheck did not report the change")
			}
		}

		write("one")
		created := plan()
		expectFast(created)
		executeCrashFixture(t, ctx, svc, app, created, ownerID)
		targets, err := svc.RollbackTargets(ctx, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]bool{}
		for _, target := range targets {
			kinds[target.Kind] = true
		}
		if !kinds["successful_sync"] {
			t.Fatalf("deployment snapshot missing: %+v", targets)
		}

		// Snapshots built from observed objects match a full live scan.
		current, err := db.ApplicationByID(ctx, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		input, err := svc.loadPlanInput(ctx, current)
		if err != nil {
			t.Fatal(err)
		}
		scanned, err := svc.captureManagedLiveState(ctx, current, input)
		if err != nil {
			t.Fatal(err)
		}
		managed, err := db.ManagedResources(ctx, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		identities := map[string]core.Identity{}
		for _, resource := range managed {
			identities[resource.Identity.Key()] = resource.Identity
		}
		if err := observeLive(ctx, input, identities); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := observedManagedState(input, managed); !ok || err != nil {
			t.Fatalf("observed state was not usable: %v %v", ok, err)
		}
		reused, err := svc.captureManagedLiveState(ctx, current, input)
		if err != nil {
			t.Fatal(err)
		}
		if len(scanned) != 3 || !reflect.DeepEqual(scanned, reused) {
			t.Fatalf("observed snapshot differs from a live scan:\n%+v\n%+v", scanned, reused)
		}

		write("two")
		updated := plan()
		if len(updated.Plan.Changes) != 3 {
			t.Fatalf("expected three updates: %+v", updated.Plan.Changes)
		}
		expectFast(updated)

		// An out-of-band edit to an unchanged field of a planned object is
		// caught by its resourceVersion.
		configMap, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, name("a"), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		configMap.Data["extra"] = "drift"
		if _, err := kubeClient.CoreV1().ConfigMaps(namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		expectFull(updated, "inputs changed")

		fresh := plan()
		expectFast(fresh)
		write("three")
		expectFull(fresh, "revision moved")
	})
}
