package clusteragent

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type environmentFixture struct {
	openshift, denyProject                                                       bool
	projectRequests                                                              int
	mu                                                                           sync.Mutex
	namespace                                                                    *corev1.Namespace
	account                                                                      *corev1.ServiceAccount
	binding                                                                      *rbacv1.RoleBinding
	namespaceCreates, accountCreates, bindingCreates, deletes, workloads, issued int
	failBinding                                                                  bool
	namespaceGone                                                                bool
}

func newEnvironmentFixture(t *testing.T) (*Agent, *environmentFixture) {
	t.Helper()
	f := &environmentFixture{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if strings.Contains(r.URL.Path, "/configmaps") {
			f.workloads++
			if r.Header.Get("Authorization") != "Bearer namespace-token" {
				t.Error("workload used provisioning credential")
				w.WriteHeader(403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "ConfigMap"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer provisioning-only" {
			t.Error("provisioning credential missing")
			w.WriteHeader(403)
			return
		}
		nsPath := "/api/v1/namespaces/preview-1"
		switch {
		case r.URL.Path == "/apis/project.openshift.io/v1":
			if f.openshift {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(404)
			}
		case r.URL.Path == "/apis/project.openshift.io/v1/projectrequests":
			f.projectRequests++
			if f.denyProject {
				w.WriteHeader(403)
				return
			}
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request["kind"] != "ProjectRequest" {
				t.Error("not a platform project request")
			}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-1", UID: "namespace-uid", ResourceVersion: "1", Annotations: map[string]string{"openshift.io/requester": "platform-issuer"}}}
			f.namespace = ns
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(ns)
		case r.URL.Path == "/api/v1/namespaces" && r.Method == "POST":
			f.namespaceCreates++
			var ns corev1.Namespace
			if err := json.NewDecoder(r.Body).Decode(&ns); err != nil {
				t.Error(err)
			}
			if f.namespace != nil && !f.namespaceGone {
				w.WriteHeader(409)
				return
			}
			ns.UID = "namespace-uid"
			ns.ResourceVersion = "1"
			f.namespace = &ns
			f.namespaceGone = false
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(ns)
		case r.URL.Path == nsPath:
			if f.namespace == nil || f.namespaceGone {
				w.WriteHeader(404)
				return
			}
			if r.Method == "DELETE" {
				var options metav1.DeleteOptions
				_ = json.NewDecoder(r.Body).Decode(&options)
				if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != f.namespace.UID || options.Preconditions.ResourceVersion == nil {
					t.Error("delete lacks UID/resourceVersion precondition")
					w.WriteHeader(409)
					return
				}
				f.deletes++
				now := metav1.Now()
				f.namespace.DeletionTimestamp = &now
				w.WriteHeader(202)
				return
			}
			_ = json.NewEncoder(w).Encode(f.namespace)
		case strings.HasSuffix(r.URL.Path, "/serviceaccounts/justcd-deployer/token"):
			f.issued++
			_ = json.NewEncoder(w).Encode(authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: "namespace-token", ExpirationTimestamp: metav1.NewTime(time.Now().Add(10 * time.Minute))}})
		case r.URL.Path == nsPath+"/serviceaccounts" && r.Method == "POST":
			f.accountCreates++
			var account corev1.ServiceAccount
			_ = json.NewDecoder(r.Body).Decode(&account)
			if account.Name != "justcd-deployer" || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
				t.Error("unsafe deployment account")
			}
			account.UID = "account-uid"
			f.account = &account
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(account)
		case r.URL.Path == nsPath+"/serviceaccounts/justcd-deployer":
			if f.account == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(f.account)
		case r.URL.Path == "/apis/rbac.authorization.k8s.io/v1/namespaces/preview-1/rolebindings" && r.Method == "POST":
			if f.failBinding {
				w.WriteHeader(403)
				return
			}
			f.bindingCreates++
			var binding rbacv1.RoleBinding
			_ = json.NewDecoder(r.Body).Decode(&binding)
			if binding.RoleRef.Kind != "ClusterRole" || binding.RoleRef.Name != "approved-deployment" {
				t.Error("unapproved role grant")
			}
			binding.UID = "binding-uid"
			f.binding = &binding
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(binding)
		case r.URL.Path == "/apis/rbac.authorization.k8s.io/v1/namespaces/preview-1/rolebindings/justcd-deployer":
			if f.binding == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(f.binding)
		default:
			t.Errorf("unexpected provisioning route: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(upstream.Close)
	policy := &agentprotocol.DeploymentCredentials{LifetimeSeconds: 600, Provisioning: &agentprotocol.NamespaceProvisioning{DeploymentClusterRole: "approved-deployment", DeleteNamespaces: true}}
	config := Config{ClusterID: "fixture", ServerURL: "https://justcd.invalid", IdentityFile: t.TempDir() + "/identity", EnrollmentTokenFile: "unused", Profiles: []agentprotocol.Profile{{Name: "managed", WorkspaceIDs: []string{"workspace"}, Namespaces: []string{"preview-*"}, DeniedNamespaces: []string{"justcd-agent"}, DeploymentCredentials: policy}}}
	kube := &rest.Config{Host: upstream.URL, BearerToken: "provisioning-only", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})}}
	a, err := New(config, kube)
	if err != nil {
		t.Fatal(err)
	}
	return a, f
}
func environmentTask() agentprotocol.Request {
	return agentprotocol.Request{Profile: "managed", WorkspaceID: "workspace", Namespace: "preview-1", Method: "POST", URI: "/justcd/environments", Deadline: time.Now().Add(time.Minute), Environment: &agentprotocol.EnvironmentRequest{ID: "environment-id", ApplicationID: "app-id", Action: "ensure", CreateNamespace: true}}
}
func ensureFixture(t *testing.T, a *Agent) agentprotocol.Result {
	t.Helper()
	result := a.Execute(context.Background(), environmentTask())
	if result.Error != "" || result.Environment == nil || result.Environment.Phase != "ready" {
		t.Fatalf("environment provisioning failed: %s", result.Error)
	}
	return result
}
func TestEnvironmentProvisioningConcurrentAndMintedWorkload(t *testing.T) {
	a, f := newEnvironmentFixture(t)
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() { ensureFixture(t, a) })
	}
	workers.Wait()
	task := managedTask()
	task.Namespace = "preview-1"
	task.URI = "/api/v1/namespaces/preview-1/configmaps/test"
	requireManagedSuccess(t, a, task)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.namespaceCreates != 1 || f.accountCreates != 1 || f.bindingCreates != 1 || f.workloads != 1 || f.issued != 1 {
		t.Fatal("concurrent provisioning was not idempotent or minted workload identity missing")
	}
	if f.namespace.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Fatal("namespace lacks restricted pod security")
	}
	info, err := os.Stat(a.Config.IdentityFile + ".environments.json")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("ownership ledger is not private and durable")
	}
}
func TestEnvironmentPartialFailureRecoversAfterRestart(t *testing.T) {
	a, f := newEnvironmentFixture(t)
	f.failBinding = true
	result := a.Execute(context.Background(), environmentTask())
	if result.Error == "" || result.Environment == nil || result.Environment.NamespaceUID != "namespace-uid" || result.Environment.FailedPhase != "role-binding" || result.Environment.Phase != "failed" {
		t.Fatal("partial provisioning failure omitted recovery metadata")
	}
	f.mu.Lock()
	f.failBinding = false
	f.mu.Unlock()
	restarted := &Agent{Config: a.Config, Local: a.Local, Workload: a.Workload, KubernetesURL: a.KubernetesURL}
	ensureFixture(t, restarted)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.namespaceCreates != 1 || f.accountCreates != 1 || f.bindingCreates != 1 {
		t.Fatal("restart recreated already managed infrastructure")
	}
}
func TestEnvironmentDeletionWaitsAndBlocksWorkloads(t *testing.T) {
	a, f := newEnvironmentFixture(t)
	created := ensureFixture(t, a)
	task := environmentTask()
	task.Environment.Action = "release"
	task.Environment.ExpectedUID = created.Environment.NamespaceUID
	result := a.Execute(context.Background(), task)
	if result.Error != "" || result.Environment.Phase != "deleting" {
		t.Fatal("namespace deletion did not enter waiting phase")
	}
	workload := managedTask()
	workload.Namespace = "preview-1"
	workload.URI = "/api/v1/namespaces/preview-1/configmaps/test"
	if a.Execute(context.Background(), workload).Error == "" {
		t.Fatal("deleting environment allowed token issuance/deployment")
	}
	result = a.Execute(context.Background(), task)
	if result.Error != "" || result.Environment.Phase != "deleting" {
		t.Fatal("finalizer retry did not retain deleting state")
	}
	f.mu.Lock()
	if f.deletes != 1 || f.issued != 0 {
		t.Error("delete retried or new token issued while finalizing")
	}
	f.namespaceGone = true
	f.mu.Unlock()
	result = a.Execute(context.Background(), task)
	if result.Error != "" || result.Environment.Phase != "deleted" {
		t.Fatal("namespace cleanup did not finish after disappearance")
	}
}
func TestEnvironmentDeletionPreservesSharedAndExistingNamespaces(t *testing.T) {
	for _, scenario := range []string{"existing", "shared-app", "server-shared", "delete-disabled", "keep"} {
		t.Run(scenario, func(t *testing.T) {
			a, f := newEnvironmentFixture(t)
			if scenario == "existing" {
				a.Config.Profiles[0].Namespaces = []string{"preview-1"}
				f.namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-1", UID: "pre-existing"}}
			}
			created := ensureFixture(t, a)
			if scenario == "shared-app" {
				other := environmentTask()
				other.Environment.ApplicationID = "other-app"
				if a.Execute(context.Background(), other).Error != "" {
					t.Fatal("shared app registration failed")
				}
			}
			if scenario == "delete-disabled" {
				a.Config.Profiles[0].DeploymentCredentials.Provisioning.DeleteNamespaces = false
			}
			task := environmentTask()
			task.Environment.Action = "release"
			task.Environment.ExpectedUID = created.Environment.NamespaceUID
			task.Environment.Shared = scenario == "server-shared"
			task.Environment.PreserveNamespace = scenario == "keep"
			result := a.Execute(context.Background(), task)
			if result.Error != "" || result.Environment.Phase != "retained" {
				t.Fatal("namespace was not preserved")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.deletes != 0 {
				t.Fatal("shared/pre-existing namespace was deleted")
			}
			if scenario == "existing" && (f.accountCreates != 0 || f.bindingCreates != 0 || f.namespaceCreates != 0) {
				t.Fatal("existing namespace/platform RBAC was modified")
			}
		})
	}
}
func TestEnvironmentOwnershipChangesFailClosed(t *testing.T) {
	for _, scenario := range []string{"namespace-uid", "annotations", "missing-ledger", "corrupt-ledger", "account-uid", "rolebinding"} {
		t.Run(scenario, func(t *testing.T) {
			a, f := newEnvironmentFixture(t)
			created := ensureFixture(t, a)
			task := environmentTask()
			task.Environment.Action = "release"
			task.Environment.ExpectedUID = created.Environment.NamespaceUID
			switch scenario {
			case "namespace-uid":
				f.namespace.UID = "recreated-uid"
			case "annotations":
				f.namespace.Annotations[environmentAnnotation] = "somebody-else"
			case "missing-ledger":
				if err := os.Remove(a.Config.IdentityFile + ".environments.json"); err != nil {
					t.Fatal(err)
				}
			case "corrupt-ledger":
				if err := os.WriteFile(a.Config.IdentityFile+".environments.json", []byte("not JSON"), 0600); err != nil {
					t.Fatal(err)
				}
			case "account-uid":
				f.account.UID = "replaced-account"
				task = managedTask()
				task.Namespace = "preview-1"
				task.URI = "/api/v1/namespaces/preview-1/configmaps/test"
			case "rolebinding":
				f.binding.RoleRef.Name = "cluster-admin"
				task = environmentTask()
			}
			if a.Execute(context.Background(), task).Error == "" {
				t.Fatal("ownership change accepted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.deletes != 0 || f.issued != 0 || f.workloads != 0 {
				t.Fatal("ownership mismatch authorized a destructive/workload operation")
			}
		})
	}
}
func TestEnvironmentLocalPolicyCannotBeBypassedByLifecycleOperation(t *testing.T) {
	for _, scenario := range []string{"workspace", "protected", "cluster-scope", "namespace", "account-choice", "manifest", "invalid-id"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := newEnvironmentFixture(t)
			task := environmentTask()
			switch scenario {
			case "workspace":
				task.WorkspaceID = "other"
			case "protected":
				task.Namespace = "kube-system"
			case "cluster-scope":
				task.ClusterScope = true
			case "namespace":
				task.Namespace = "production"
			case "account-choice":
				task.URI = "/api/v1/namespaces/preview-1/serviceaccounts/default/token"
			case "manifest":
				task.Body = []byte(`{"kind":"ClusterRoleBinding"}`)
			case "invalid-id":
				task.Environment.ID = "../escape"
			}
			if a.Execute(context.Background(), task).ErrorCode != "cluster.agent_scope_denied" {
				t.Fatal("lifecycle operation bypassed local policy")
			}
		})
	}
}

func TestEnvironmentOpenShiftUsesProjectRequestAndPreservesPlatformMetadata(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "project-created", true: "project-denied"}[denied], func(t *testing.T) {
			a, f := newEnvironmentFixture(t)
			f.openshift = true
			f.denyProject = denied
			a.Config.Profiles[0].DeploymentCredentials.Provisioning.Platform = "openshift"
			result := a.Execute(context.Background(), environmentTask())
			if denied {
				if result.Error == "" {
					t.Fatal("denied ProjectRequest succeeded")
				}
			} else {
				if result.Error != "" || result.Environment.Phase != "ready" {
					t.Fatalf("managed OpenShift provisioning failed: %s", result.Error)
				}
				if f.namespace.Annotations["openshift.io/requester"] != "platform-issuer" || len(f.namespace.Annotations) != 1 {
					t.Fatal("platform metadata was overwritten")
				}
				task := environmentTask()
				task.Environment.Action = "release"
				task.Environment.ExpectedUID = result.Environment.NamespaceUID
				result = a.Execute(context.Background(), task)
				if result.Error != "" || result.Environment.Phase != "deleting" {
					t.Fatal("UID-backed project cleanup failed")
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.namespaceCreates != 0 || f.projectRequests != 1 {
				t.Fatal("ProjectRequest path fell back to Namespace creation")
			}
		})
	}
}
