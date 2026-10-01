package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var namespacesGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
var resourceQuotasGVR = schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"}
var errNamespaceDeleting = errors.New("preview namespace deletion is in progress")

func previewIdentity(connectionID string, number int, prefix string) (string, string) {
	sum := sha256.Sum256([]byte(connectionID + ":" + strconv.Itoa(number)))
	hash := hex.EncodeToString(sum[:6])
	namespace := prefix + "-pr-" + strconv.Itoa(number) + "-" + hash
	if len(namespace) > 63 {
		namespace = namespace[:63-len(hash)-1] + "-" + hash
	}
	return "preview-" + hash, namespace
}

func renderPreviewValue(value, namespace string, number int, sha string) string {
	r := strings.NewReplacer("{{namespace}}", namespace, "{{number}}", strconv.Itoa(number), "{{sha}}", sha)
	return r.Replace(value)
}

func pullRequestRef(provider string, number int) string {
	if provider == "gitlab" {
		return "refs/merge-requests/" + strconv.Itoa(number) + "/head"
	}
	return "refs/pull/" + strconv.Itoa(number) + "/head"
}

func (s *Server) previewApplication(ctx context.Context, production store.Application, connection store.SourceControlConnection, review store.PullRequestReview) (store.Application, string, error) {
	p := connection.PreviewProfile
	if review.PreviewApplicationID != nil {
		existing, err := s.Store.ApplicationByID(ctx, *review.PreviewApplicationID)
		if err != nil {
			return store.Application{}, "", err
		}
		if existing.WorkspaceID != production.WorkspaceID || existing.SourceID != production.SourceID || existing.ClusterID != production.ClusterID {
			return store.Application{}, "", errors.New("PR deployment target changed")
		}
		if !review.SharedEnvironment && len(existing.Namespaces) != 1 {
			return store.Application{}, "", errors.New("preview needs a dedicated namespace")
		}
		return existing, existing.Namespaces[0].Namespace, nil
	}
	if p.DeploymentMode == "existing" {
		if err := s.Store.AttachSharedPR(ctx, review, production, pullRequestRef(connection.Provider, review.Number)); err != nil {
			return store.Application{}, "", err
		}
		app, err := s.Store.ApplicationByID(ctx, production.ID)
		return app, "", err
	}
	id, namespace := previewIdentity(connection.ID, review.Number, p.NamespacePrefix)
	newSlot, err := s.Store.ReservePreviewSlot(ctx, connection.ID, review.Number, p.MaxActive)
	if err != nil {
		return store.Application{}, "", err
	}
	keepSlot := false
	defer func() {
		if newSlot && !keepSlot {
			_ = s.Store.ReleasePreviewSlot(ctx, connection.ID, review.Number)
		}
	}()
	if existing, err := s.Store.ApplicationByID(ctx, id); err == nil {
		if existing.WorkspaceID != production.WorkspaceID || existing.SourceID != production.SourceID || existing.ClusterID != production.ClusterID || len(existing.Namespaces) != 1 || existing.Namespaces[0].Namespace != namespace {
			return store.Application{}, "", errors.New("preview application identity collision")
		}
		if existing.Revision != pullRequestRef(connection.Provider, review.Number) || existing.TargetHelmValuesYAML != renderPreviewValue(p.HelmValuesYAML, namespace, review.Number, review.HeadSHA) {
			existing.Revision = pullRequestRef(connection.Provider, review.Number)
			existing.TargetHelmValuesYAML = renderPreviewValue(p.HelmValuesYAML, namespace, review.Number, review.HeadSHA)
			if err := s.Store.UpdateApplication(ctx, existing); err != nil {
				return store.Application{}, "", err
			}
		}
		if review.Fork && existing.ApprovalPolicyOverride == nil {
			rule := store.ApprovalRule{RequiredApprovals: 1, ApproverRoles: []string{"owner"}}
			override := &store.ApprovalPolicyOverride{Sync: &rule}
			if err := s.Store.UpdateApplicationApprovalPolicyOverride(ctx, existing.ID, override); err != nil {
				return store.Application{}, "", err
			}
			existing.ApprovalPolicyOverride = override
		}
		keepSlot = true
		return existing, namespace, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Application{}, "", err
	}
	// A workspace binding may already exist after an interrupted first attempt.
	binding, err := s.Store.NamespaceBinding(ctx, production.WorkspaceID, production.ClusterID, namespace)
	if errors.Is(err, sql.ErrNoRows) {
		if err = s.Store.CreateNamespaceBinding(ctx, production.WorkspaceID, production.ClusterID, namespace, nil); err != nil {
			return store.Application{}, "", err
		}
		binding = store.NamespaceBinding{Namespace: namespace}
	} else if err != nil {
		return store.Application{}, "", err
	}
	preview := store.Application{ID: id, WorkspaceID: production.WorkspaceID, Name: id, SourceID: production.SourceID, Revision: pullRequestRef(connection.Provider, review.Number), ManifestPath: production.ManifestPath, Renderer: production.Renderer, KustomizeHelmEnabled: production.KustomizeHelmEnabled, KustomizeNamespaceOverride: production.Renderer == "kustomize", ClusterID: production.ClusterID, Namespaces: []store.NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 86400, RetryPolicy: store.DefaultRetryPolicy(), Health: "unknown"}
	if p.ManifestPath != "" {
		preview.ManifestPath = p.ManifestPath
	}
	if preview.Renderer == "helm" {
		preview.HelmValuesFiles = p.HelmValuesFiles
		preview.TargetHelmValuesYAML = renderPreviewValue(p.HelmValuesYAML, namespace, review.Number, review.HeadSHA)
	}
	if err := s.Store.CreateApplication(ctx, preview); err != nil {
		return store.Application{}, "", err
	}
	keepSlot = true
	if review.Fork {
		rule := store.ApprovalRule{RequiredApprovals: 1, ApproverRoles: []string{"owner"}}
		override := &store.ApprovalPolicyOverride{Sync: &rule}
		if err := s.Store.UpdateApplicationApprovalPolicyOverride(ctx, preview.ID, override); err != nil {
			return store.Application{}, "", err
		}
		preview.ApprovalPolicyOverride = override
	}
	_ = s.Store.Audit(ctx, reviewActorID, "preview.application_created", "application", preview.ID, map[string]any{"pullRequest": review.Number, "namespace": namespace, "sourceApplicationId": production.ID})
	return preview, namespace, nil
}

func validatePreviewResources(resources []core.Resource, profile store.PreviewProfile, namespace string, fork bool) error {
	allowed := map[string]bool{}
	for _, name := range profile.AllowedSecrets {
		allowed[name] = true
	}
	for _, resource := range resources {
		if resource.Identity.ClusterScoped || resource.Identity.Namespace != namespace {
			return fmt.Errorf("preview resource %s/%s escapes its namespace", resource.Identity.Kind, resource.Identity.Name)
		}
		switch resource.Identity.Kind {
		case "Role", "RoleBinding", "ServiceAccount", "NetworkPolicy", "ResourceQuota", "LimitRange", "PersistentVolumeClaim":
			return fmt.Errorf("preview manifests may not manage %s resources", resource.Identity.Kind)
		}
		if resource.Identity.Kind == "Secret" || resource.Identity.Kind == "ExternalSecret" {
			return errors.New("preview manifests may not create or clone secrets")
		}
		var manifest map[string]any
		if err := json.Unmarshal(resource.Manifest, &manifest); err != nil {
			return err
		}
		if err := checkPreviewSecrets(manifest, allowed); err != nil {
			return err
		}
		if hasServiceAccountToken(manifest) {
			return errors.New("preview workloads must disable service account token mounting")
		}
		if hasUnsafePodAccess(manifest) {
			return errors.New("preview workloads may not use host access or privileged containers")
		}
		spec, _ := manifest["spec"].(map[string]any)
		if resource.Identity.Kind == "Service" {
			if typ, _ := spec["type"].(string); typ == "LoadBalancer" || typ == "NodePort" || typ == "ExternalName" {
				return errors.New("preview services must remain inside the cluster")
			}
		}
		if resource.Identity.Kind == "Ingress" {
			if err := checkIngressHosts(spec, namespace, profile.HostSuffix); err != nil {
				return err
			}
		}
		if resource.Identity.Kind == "Gateway" || resource.Identity.Kind == "HTTPRoute" || resource.Identity.Kind == "VirtualService" {
			return errors.New("preview exposure currently requires Kubernetes Ingress")
		}
	}
	return nil
}

func hasServiceAccountToken(value any) bool {
	switch node := value.(type) {
	case map[string]any:
		if _, ok := node["serviceAccountToken"]; ok {
			return true
		}
		if _, ok := node["containers"]; ok {
			if automount, ok := node["automountServiceAccountToken"].(bool); !ok || automount {
				return true
			}
		}
		for _, item := range node {
			if hasServiceAccountToken(item) {
				return true
			}
		}
	case []any:
		for _, item := range node {
			if hasServiceAccountToken(item) {
				return true
			}
		}
	}
	return false
}

func hasUnsafePodAccess(value any) bool {
	switch node := value.(type) {
	case map[string]any:
		for _, key := range []string{"hostNetwork", "hostPID", "hostIPC", "privileged", "allowPrivilegeEscalation"} {
			if enabled, _ := node[key].(bool); enabled {
				return true
			}
		}
		if _, ok := node["hostPath"]; ok {
			return true
		}
		for _, item := range node {
			if hasUnsafePodAccess(item) {
				return true
			}
		}
	case []any:
		for _, item := range node {
			if hasUnsafePodAccess(item) {
				return true
			}
		}
	}
	return false
}

func checkPreviewSecrets(value any, allowed map[string]bool) error {
	switch node := value.(type) {
	case map[string]any:
		for key, item := range node {
			if key == "secretKeyRef" || key == "secretRef" || key == "secret" {
				ref, _ := item.(map[string]any)
				if name, _ := ref["name"].(string); name != "" && !allowed[name] {
					return fmt.Errorf("preview secret %q is not allowed", name)
				}
			}
			if key == "secretName" {
				if name, _ := item.(string); name != "" && !allowed[name] {
					return fmt.Errorf("preview secret %q is not allowed", name)
				}
			}
			if key == "imagePullSecrets" {
				if refs, ok := item.([]any); ok {
					for _, v := range refs {
						ref, _ := v.(map[string]any)
						if name, _ := ref["name"].(string); name != "" && !allowed[name] {
							return fmt.Errorf("preview secret %q is not allowed", name)
						}
					}
				}
			}
			if err := checkPreviewSecrets(item, allowed); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range node {
			if err := checkPreviewSecrets(item, allowed); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkIngressHosts(spec map[string]any, namespace, suffix string) error {
	base := namespace + "." + suffix
	valid := func(host string) bool { return host == base || strings.HasSuffix(host, "."+base) }
	rules, _ := spec["rules"].([]any)
	if len(rules) == 0 {
		return errors.New("preview ingress needs explicit hosts")
	}
	for _, item := range rules {
		rule, _ := item.(map[string]any)
		host, _ := rule["host"].(string)
		if !valid(host) {
			return fmt.Errorf("preview ingress host %q is outside %q", host, base)
		}
	}
	if tls, _ := spec["tls"].([]any); len(tls) > 0 {
		for _, item := range tls {
			entry, _ := item.(map[string]any)
			hosts, _ := entry["hosts"].([]any)
			for _, host := range hosts {
				if h, _ := host.(string); !valid(h) {
					return fmt.Errorf("preview TLS host %q is outside %q", h, base)
				}
			}
		}
	}
	return nil
}

func (s *Server) ensurePreviewNamespace(ctx context.Context, connection store.SourceControlConnection, review store.PullRequestReview, namespace string) error {
	app, err := s.Store.ApplicationByID(ctx, connection.ApplicationID)
	if err != nil {
		return err
	}
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return err
	}
	if cluster.ClusterScopeCredential == nil && !kube.AgentClusterScope(ctx, s.Store, cluster.ID) {
		return errors.New("preview namespace creation requires a cluster-scope credential")
	}
	client, err := kube.ForWorkspaceBinding(ctx, s.Store, s.EncryptionKey, cluster, cluster.ClusterScopeCredential, true, app.WorkspaceID, namespace)
	if err != nil {
		return err
	}
	labels := map[string]any{"justcd.io/preview-connection": connection.ID, "justcd.io/preview-number": strconv.Itoa(review.Number)}
	resource := client.Dynamic.Resource(namespacesGVR)
	existing, err := resource.Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": namespace, "labels": labels}}}
		if _, err = resource.Create(ctx, obj, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if existing.GetLabels()["justcd.io/preview-connection"] != connection.ID || existing.GetLabels()["justcd.io/preview-number"] != strconv.Itoa(review.Number) {
			return errors.New("preview namespace is owned by another deployment")
		}
	}
	quota := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ResourceQuota", "metadata": map[string]any{"name": "justcd-preview-quota", "namespace": namespace, "labels": labels}, "spec": map[string]any{"hard": map[string]any{"requests.cpu": connection.PreviewProfile.QuotaCPU, "requests.memory": connection.PreviewProfile.QuotaMemory, "limits.cpu": connection.PreviewProfile.QuotaCPU, "limits.memory": connection.PreviewProfile.QuotaMemory}}}}
	quotas := client.Dynamic.Resource(resourceQuotasGVR).Namespace(namespace)
	old, err := quotas.Get(ctx, "justcd-preview-quota", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = quotas.Create(ctx, quota, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if old.GetLabels()["justcd.io/preview-connection"] != connection.ID {
		return errors.New("preview quota is owned by another deployment")
	}
	quota.SetResourceVersion(old.GetResourceVersion())
	_, err = quotas.Update(ctx, quota, metav1.UpdateOptions{})
	return err
}

func (s *Server) removePreviewNamespace(ctx context.Context, connection store.SourceControlConnection, review store.PullRequestReview, namespace string) error {
	app, err := s.Store.ApplicationByID(ctx, connection.ApplicationID)
	if err != nil {
		return err
	}
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return err
	}
	client, err := kube.ForWorkspaceBinding(ctx, s.Store, s.EncryptionKey, cluster, cluster.ClusterScopeCredential, true, app.WorkspaceID, namespace)
	if err != nil {
		return err
	}
	resource := client.Dynamic.Resource(namespacesGVR)
	old, err := resource.Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if old.GetLabels()["justcd.io/preview-connection"] != connection.ID || old.GetLabels()["justcd.io/preview-number"] != strconv.Itoa(review.Number) {
		return errors.New("preview namespace ownership changed; refusing cleanup")
	}
	uid := old.GetUID()
	if err := resource.Delete(ctx, namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return errNamespaceDeleting
}
