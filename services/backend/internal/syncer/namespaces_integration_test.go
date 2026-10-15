//go:build integration

package syncer

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// Exercise the complete planner so workload reads cannot accidentally move
// ahead of namespace prerequisite validation again.
func TestIntegrationPlanMissingNamespaceBeforeWorkloadReads(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		exists, disabled, denied bool
		wantResource, wantAction string
	}{
		{name: "missing namespace plans reviewed creation"},
		{name: "existing namespace preserves workload denial", exists: true, wantResource: "ConfigMap", wantAction: "get"},
		{name: "disabled creation preserves workload denial", disabled: true, wantResource: "ConfigMap", wantAction: "get"},
		{name: "namespace read denied", denied: true, wantResource: "Namespace", wantAction: "get"},
		{name: "namespace dry-run denied", wantResource: "Namespace", wantAction: "create (dry-run)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := isolatedPlannerStore(t)
			repo := newIntegrationGitRepo(t, "shop")
			var workloadReads, namespaceDryRuns atomic.Int32
			clusterAPI := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch r.URL.Path {
				case "/version":
					response = map[string]string{"major": "1", "minor": "32", "gitVersion": "v1.32.0"}
				case "/api":
					response = map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}}
				case "/apis":
					response = map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}}
				case "/api/v1":
					response = map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{
						map[string]any{"name": "namespaces", "kind": "Namespace", "namespaced": false, "verbs": []string{"get", "create"}},
						map[string]any{"name": "configmaps", "kind": "ConfigMap", "namespaced": true, "verbs": []string{"get", "list", "create", "patch"}},
					}}
				case "/api/v1/namespaces/shop":
					if tc.denied {
						w.WriteHeader(http.StatusForbidden)
						response = map[string]any{"kind": "Status", "apiVersion": "v1", "reason": "Forbidden", "code": 403}
					} else if tc.exists {
						response = map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]string{"name": "shop"}}
					} else {
						w.WriteHeader(http.StatusNotFound)
						response = map[string]any{"kind": "Status", "apiVersion": "v1", "reason": "NotFound", "code": 404}
					}
				case "/api/v1/namespaces":
					if r.Method != http.MethodPost || r.URL.Query().Get("dryRun") != "All" {
						t.Error("planning attempted a real namespace mutation")
					}
					namespaceDryRuns.Add(1)
					if tc.wantAction == "create (dry-run)" {
						w.WriteHeader(http.StatusForbidden)
						response = map[string]any{"kind": "Status", "apiVersion": "v1", "reason": "Forbidden", "code": 403}
					} else {
						response = map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]string{"name": "shop"}}
					}
				default:
					if strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/shop/configmaps") {
						workloadReads.Add(1)
						w.WriteHeader(http.StatusForbidden)
						response = map[string]any{"kind": "Status", "apiVersion": "v1", "reason": "Forbidden", "code": 403}
					} else {
						t.Errorf("unexpected cluster request: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusNotFound)
					}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer clusterAPI.Close()
			key := []byte(strings.Repeat("k", 32))
			cipher, err := security.Encrypt(key, []byte(`{"token":"workload-token"}`), "credential:namespace-kube")
			if err != nil {
				t.Fatal(err)
			}
			workspace, credentialID := "workspace", "namespace-kube"
			if err := db.CreateCredential(ctx, store.Credential{ID: credentialID, WorkspaceID: &workspace, Name: "Namespace regression", Kind: "kubernetes-token", Cipher: cipher}); err != nil {
				t.Fatal(err)
			}
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clusterAPI.Certificate().Raw})
			if _, err := db.DB.ExecContext(ctx, `UPDATE clusters SET api_server=$1,ca_data=$2,default_credential_id=$3 WHERE id='cluster'`, clusterAPI.URL, ca, credentialID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.ExecContext(ctx, `UPDATE git_sources SET repository_url=$1 WHERE id='source'`, repo.url); err != nil {
				t.Fatal(err)
			}
			if err := db.CreateNamespaceBinding(ctx, workspace, "cluster", "shop", &credentialID); err != nil {
				t.Fatal(err)
			}
			app := store.Application{ID: "namespace-app", WorkspaceID: workspace, SourceID: "source", Name: "Namespace regression", Revision: "main", ManifestPath: "yaml", Renderer: "yaml", ClusterID: "cluster", Namespaces: []store.NamespaceBinding{{Namespace: "shop"}}, CreateNamespaces: !tc.disabled, SyncPolicy: "auto-safe", PollSeconds: 300}
			if err := db.CreateApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
			svc := &Service{Store: db, EncryptionKey: key}
			plan, _, err := svc.CalculatePlan(ctx, app)
			if tc.wantResource != "" {
				if err == nil {
					t.Fatal("permission denial was ignored")
				}
				issue := PlanFailureDiagnostic(err)
				if issue.Code != "kubernetes.permission_denied" || issue.Resource == nil || issue.Resource.Kind != tc.wantResource || issue.Action != tc.wantAction {
					t.Fatalf("wrong diagnostic: %+v (error: %v)", issue, err)
				}
				if tc.wantResource == "Namespace" && workloadReads.Load() != 0 {
					t.Fatal("workloads read before validating namespace access")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if workloadReads.Load() != 0 || namespaceDryRuns.Load() != 1 {
				t.Fatalf("unexpected API calls: workload reads=%d namespace dry-runs=%d", workloadReads.Load(), namespaceDryRuns.Load())
			}
			if len(plan.NamespaceCreations) != 1 || len(plan.Changes) != 3 || !plan.RequiresApproval {
				t.Fatalf("missing reviewed namespace/workload creations: %+v", plan)
			}
			for _, change := range plan.Changes {
				if change.Kind != core.Create {
					t.Fatalf("unexpected change: %+v", change)
				}
			}
		})
	}
}
