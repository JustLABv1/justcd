package clusteragent

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Opt-in only: creates two temporary namespaces using the explicitly supplied
// administrator kubeconfig. It never consults the user's default kubeconfig.
func TestManagedCredentialsRealCluster(t *testing.T) {
	path := os.Getenv("JUSTCD_MANAGED_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("set JUSTCD_MANAGED_TEST_KUBECONFIG to run real TokenRequest/RBAC validation")
	}
	adminConfig, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal("could not load explicit test kubeconfig")
	}
	admin, err := kubernetes.NewForConfig(adminConfig)
	if err != nil {
		t.Fatal("could not construct test administrator client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	namespace := fmt.Sprintf("justcd-token-test-%d", time.Now().UnixNano())
	for _, name := range []string{namespace, namespace + "-other"} {
		ns, err := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal("could not create test namespace")
		}
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			uid := ns.UID
			if err := admin.CoreV1().Namespaces().Delete(cleanup, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
				t.Error("could not remove test namespace with UID precondition")
			}
		})
	}
	for _, name := range []string{agentprotocol.DeploymentServiceAccount, "token-helper"} {
		if _, err = admin.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{}); err != nil {
			t.Fatal("could not prepare service accounts")
		}
	}
	roles := []rbacv1.Role{
		{ObjectMeta: metav1.ObjectMeta{Name: "issuer"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"}, ResourceNames: []string{agentprotocol.DeploymentServiceAccount}, Verbs: []string{"create"}}, {APIGroups: []string{""}, Resources: []string{"serviceaccounts"}, ResourceNames: []string{agentprotocol.DeploymentServiceAccount}, Verbs: []string{"get"}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "deployment"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}}}},
	}
	for _, role := range roles {
		if _, err = admin.RbacV1().Roles(namespace).Create(ctx, &role, metav1.CreateOptions{}); err != nil {
			t.Fatal("could not prepare namespace role")
		}
		subject := agentprotocol.DeploymentServiceAccount
		if role.Name == "issuer" {
			subject = "token-helper"
		}
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: subject, Namespace: namespace}}}
		if _, err = admin.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
			t.Fatal("could not prepare namespace role binding")
		}
	}
	lifetime := int64(600)
	issuerToken, err := admin.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, "token-helper", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("could not mint test helper identity")
	}
	issuerConfig := rest.AnonymousClientConfig(adminConfig)
	issuerConfig.BearerToken = issuerToken.Status.Token
	issuer, err := kubernetes.NewForConfig(issuerConfig)
	if err != nil {
		t.Fatal("could not construct helper client")
	}
	// Kubernetes authorization can take a short time to observe newly created RBAC.
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err = issuer.CoreV1().ServiceAccounts(namespace).Get(ctx, agentprotocol.DeploymentServiceAccount, metav1.GetOptions{})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("minimal helper RBAC did not become effective")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, target := range []struct{ ns, account string }{{namespace, "default"}, {namespace + "-other", agentprotocol.DeploymentServiceAccount}} {
		_, err = issuer.CoreV1().ServiceAccounts(target.ns).CreateToken(ctx, target.account, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}}, metav1.CreateOptions{})
		if !apierrors.IsForbidden(err) {
			t.Fatal("helper could mint an unapproved account token")
		}
	}
	if _, err = issuer.RbacV1().RoleBindings(namespace).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "escalation"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "token-helper", Namespace: namespace}}}, metav1.CreateOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("helper could grant privileged RBAC")
	}
	// New strips this issuer's authentication before executing workload requests.
	a, err := New(Config{ClusterID: "integration", ServerURL: "https://justcd.invalid", IdentityFile: t.TempDir() + "/identity", EnrollmentTokenFile: "unused", Profiles: []agentprotocol.Profile{{Name: "managed", WorkspaceIDs: []string{"workspace"}, Namespaces: []string{namespace}, DeploymentCredentials: &agentprotocol.DeploymentCredentials{LifetimeSeconds: 600}}}}, issuerConfig)
	if err != nil {
		t.Fatal("could not construct managed agent")
	}
	task := managedTask()
	task.Namespace = namespace
	task.URI = "/api/v1/namespaces/" + namespace + "/configmaps"
	task.Method = "POST"
	task.ContentType = "application/json"
	task.Body = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"managed-test"},"data":{"test":"ok"}}`)
	result := a.Execute(ctx, task)
	if result.Status != 201 || result.Error != "" {
		t.Fatalf("issued credential did not create workload: status=%d code=%s", result.Status, result.ErrorCode)
	}
	credential, err := a.deploymentCredential(ctx, task, a.Config.Profiles[0])
	if err != nil {
		t.Fatal("could not obtain cached issued credential")
	}
	if !credential.expires.After(time.Now()) {
		t.Fatal("granted expiration is invalid")
	}
	deploymentConfig := rest.AnonymousClientConfig(adminConfig)
	deploymentConfig.BearerToken = credential.token
	deployment, err := kubernetes.NewForConfig(deploymentConfig)
	if err != nil {
		t.Fatal("could not construct minted-token client")
	}
	if _, err = deployment.CoreV1().ConfigMaps(namespace).Get(ctx, "managed-test", metav1.GetOptions{}); err != nil {
		t.Fatal("minted token cannot read its workload")
	}
	if _, err = deployment.CoreV1().ConfigMaps(namespace+"-other").List(ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token escaped namespace")
	}
	if _, err = deployment.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token could read Secrets")
	}
	if _, err = deployment.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token could read RBAC")
	}
	if _, err = deployment.RbacV1().RoleBindings(namespace).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "escalation"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: agentprotocol.DeploymentServiceAccount, Namespace: namespace}}}, metav1.CreateOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token could change RBAC")
	}
	if _, err = deployment.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token could access cluster resources")
	}
	if _, err = deployment.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, agentprotocol.DeploymentServiceAccount, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}}, metav1.CreateOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("minted token could issue credentials")
	}
	// A long operation obtains fresh credentials per request, including rollback
	// patches and cleanup; expiry does not trigger replay of a mutation.
	a.credentials.clear()
	task.Method = "PATCH"
	task.URI += "/managed-test"
	task.ContentType = "application/merge-patch+json"
	task.Body = []byte(`{"data":{"test":"rollback"}}`)
	if result = a.Execute(ctx, task); result.Status != 200 {
		t.Fatal("refresh/rollback patch failed")
	}
	task.Method = "DELETE"
	task.ContentType = "application/json"
	task.Body = nil
	if result = a.Execute(ctx, task); result.Status != 200 {
		t.Fatal("issued credential could not delete workload")
	}
}
