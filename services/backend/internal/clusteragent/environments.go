package clusteragent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const environmentAnnotation = "justcd.io/environment"

type environmentRecord struct {
	Platform                                                               string
	ID, Namespace, Workspace, Profile, OwnerApplication, ServiceAccountUID string
	Prepared                                                               bool
	agentprotocol.EnvironmentStatus
}
type environmentLedger struct {
	mu    sync.Mutex
	locks [64]sync.Mutex
}

func (a *Agent) environmentLock(namespace string) *sync.Mutex {
	sum := sha256.Sum256([]byte(namespace))
	return &a.environments.locks[int(sum[0])%len(a.environments.locks)]
}
func (a *Agent) readEnvironment(namespace string) (*environmentRecord, error) {
	a.environments.mu.Lock()
	defer a.environments.mu.Unlock()
	records, err := a.readEnvironments()
	if err != nil {
		return nil, err
	}
	return records[namespace], nil
}
func (a *Agent) readEnvironments() (map[string]*environmentRecord, error) {
	records := map[string]*environmentRecord{}
	data, err := os.ReadFile(a.Config.IdentityFile + ".environments.json")
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, errors.New("environment ownership ledger unavailable")
	}
	if json.Unmarshal(data, &records) != nil || records == nil {
		return nil, errors.New("environment ownership ledger is invalid; restore it before provisioning or cleanup")
	}
	return records, nil
}
func (a *Agent) saveEnvironment(record *environmentRecord) error {
	a.environments.mu.Lock()
	defer a.environments.mu.Unlock()
	records, err := a.readEnvironments()
	if err != nil {
		return err
	}
	records[record.Namespace] = record
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	path := a.Config.IdentityFile + ".environments.json"
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0700) != nil {
		return errors.New("environment ownership directory unavailable")
	}
	file, err := os.CreateTemp(dir, ".environments-")
	if err != nil {
		return errors.New("cannot persist environment ownership")
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err == nil {
		var directory *os.File
		directory, err = os.Open(dir)
		if err == nil {
			err = directory.Sync()
			directory.Close()
		}
	}
	if err != nil {
		return errors.New("cannot persist environment ownership; refusing further mutation")
	}
	return nil
}
func (a *Agent) provisionRequest(ctx context.Context, p agentprotocol.Profile, method, path string, body, out any) (int, error) {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.KubernetesURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, errors.New("invalid provisioning request")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.Local[p.Name].Do(req)
	if err != nil {
		return 0, errors.New("provisioning API unavailable; retry to inspect the previous outcome")
	}
	defer res.Body.Close()
	// Provisioning errors must never include upstream bodies or credential data.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, nil
	}
	if out != nil {
		data, err = io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
		if err != nil || len(data) > 1<<20 || json.Unmarshal(data, out) != nil {
			return res.StatusCode, errors.New("invalid provisioning API response")
		}
	}
	return res.StatusCode, nil
}
func requireProvisionStatus(status int, err error) error {
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("provisioning Kubernetes API returned HTTP %d; check bootstrap permissions and retry", status)
	}
	return nil
}
func (a *Agent) executeEnvironment(ctx context.Context, task agentprotocol.Request, p agentprotocol.Profile) agentprotocol.Result {
	result := agentprotocol.Result{Lease: task.Lease, ErrorCode: "cluster.agent_provisioning_failed"}
	record, err := a.ensureEnvironment(ctx, task, p)
	if record != nil {
		status := record.EnvironmentStatus
		result.Environment = &status
	}
	if err != nil {
		result.Error = err.Error()
		slog.Warn("environment lifecycle failed", "workspace", task.WorkspaceID, "namespace", task.Namespace, "application", task.Environment.ApplicationID, "action", task.Environment.Action, "reason", result.Error)
	} else {
		result.ErrorCode = ""
		result.Status = 200
		result.ContentType = "application/json"
		result.Body, _ = json.Marshal(result.Environment)
		slog.Info("environment lifecycle completed", "workspace", task.WorkspaceID, "namespace", task.Namespace, "application", task.Environment.ApplicationID, "action", task.Environment.Action, "phase", record.Phase, "namespaceUID", record.NamespaceUID)
	}
	return result
}
func (a *Agent) ensureEnvironment(ctx context.Context, task agentprotocol.Request, p agentprotocol.Profile) (record *environmentRecord, err error) {
	e := task.Environment
	record, err = a.readEnvironment(task.Namespace)
	if err != nil {
		return nil, err
	}
	if record != nil && (record.Workspace != task.WorkspaceID || record.Profile != p.Name) {
		return record, errors.New("namespace ownership belongs to another local workspace or profile")
	}
	if record != nil && record.ID != e.ID {
		if record.Phase != "deleted" {
			return record, errors.New("namespace environment identity changed; refusing adoption")
		}
		record = nil
	}
	if e.Action == "release" {
		return a.releaseEnvironment(ctx, task, p, record)
	}
	if record != nil && (record.Phase == "deleting" || record.Phase == "deleted") {
		return record, errors.New("namespace environment is deleting or deleted; finish cleanup before deploying")
	}
	if record == nil && e.ExpectedUID != "" {
		return nil, errors.New("persisted environment ownership is missing from the agent ledger; restore it before provisioning")
	}
	if record == nil {
		record = &environmentRecord{ID: e.ID, Namespace: task.Namespace, Workspace: task.WorkspaceID, Profile: p.Name, OwnerApplication: e.ApplicationID, EnvironmentStatus: agentprotocol.EnvironmentStatus{Phase: "pending", Shared: e.Shared}}
	}
	// Save failed phases and already-created resources so retries are reviewable.
	defer func() {
		if err != nil {
			record.Reason = err.Error()
			if record.Phase != "deleting" {
				record.Phase = "failed"
			}
			if saveErr := a.saveEnvironment(record); saveErr != nil {
				err = saveErr
			}
		}
	}()
	record.Shared = record.Shared || e.Shared || record.OwnerApplication != e.ApplicationID
	var namespace corev1.Namespace
	code, getErr := a.provisionRequest(ctx, p, "GET", "/api/v1/namespaces/"+task.Namespace, nil, &namespace)
	if getErr != nil {
		return record, getErr
	}
	if code == 404 {
		if record.NamespaceUID != "" || e.ExpectedUID != "" {
			return record, errors.New("owned namespace disappeared; refusing to recreate it under an old environment identity")
		}
		if !e.CreateNamespace {
			return record, errors.New("namespace is missing; enable spec.destination.createNamespace and the local provisioning policy")
		}
		// ProjectRequest runs platform templates; never bypass it with a bare
		// Namespace, and never overwrite template-generated bindings or labels.
		platform, platformErr := a.provisionRequest(ctx, p, "GET", "/apis/project.openshift.io/v1", nil, nil)
		if platformErr != nil {
			return record, platformErr
		}
		openshift := p.DeploymentCredentials.Provisioning.Platform == "openshift"
		if (openshift && platform != 200) || (!openshift && platform != 404) {
			return record, errors.New("provisioning platform does not match cluster discovery; configure openshift for ProjectRequest or kubernetes for Namespace creation")
		}
		record.Phase = "provisioning"
		record.Managed = true
		record.FailedPhase = "namespace"
		if openshift {
			record.Platform = "openshift"
		}
		if err = a.saveEnvironment(record); err != nil {
			return record, err
		}
		if openshift {
			request := struct {
				APIVersion string            `json:"apiVersion"`
				Kind       string            `json:"kind"`
				Metadata   metav1.ObjectMeta `json:"metadata"`
			}{"project.openshift.io/v1", "ProjectRequest", metav1.ObjectMeta{Name: task.Namespace}}
			code, err = a.provisionRequest(ctx, p, "POST", "/apis/project.openshift.io/v1/projectrequests", request, &namespace)
		} else {
			desired := &corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: task.Namespace, Annotations: map[string]string{environmentAnnotation: e.ID, "justcd.io/workspace": task.WorkspaceID, "justcd.io/application": record.OwnerApplication}, Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"}}}
			code, err = a.provisionRequest(ctx, p, "POST", "/api/v1/namespaces", desired, &namespace)
		}
		if err = requireProvisionStatus(code, err); err != nil {
			return record, err
		}
		if namespace.Name != task.Namespace {
			return record, errors.New("provisioner returned an unexpected namespace identity")
		}
		if namespace.UID == "" {
			return record, errors.New("created namespace has no UID; ownership cannot be established")
		}
		record.NamespaceUID = string(namespace.UID)
		record.Resources = []string{"Namespace"}
		if err = a.saveEnvironment(record); err != nil {
			return record, err
		}
	} else if err = requireProvisionStatus(code, nil); err != nil {
		return record, err
	} else if record.NamespaceUID == "" {
		if record.Managed || namespace.Annotations[environmentAnnotation] == e.ID {
			return record, errors.New("namespace creation outcome is unknown and its UID was not persisted; administrator verification is required")
		}
		explicit := false
		for _, name := range p.Namespaces {
			if name == task.Namespace {
				explicit = true
			}
		}
		if !explicit {
			return record, errors.New("pre-existing namespaces require an exact local profile entry and administrator-prepared deployment RBAC; refusing automatic adoption")
		}
		// Never adopt/modify an existing namespace, even when creation was requested.
		record.Prepared = true
		record.Managed = false
		record.Shared = true
		record.NamespaceUID = string(namespace.UID)
	}
	if namespace.UID == "" {
		return record, errors.New("namespace UID is missing; refusing provisioning")
	}
	if namespace.DeletionTimestamp != nil {
		return record, errors.New("namespace is terminating; wait for cleanup before deploying")
	}
	if string(namespace.UID) != record.NamespaceUID || (e.ExpectedUID != "" && e.ExpectedUID != record.NamespaceUID) {
		return record, errors.New("namespace UID changed; refusing provisioning")
	}
	if record.Prepared {
		record.Phase = "ready"
		record.FailedPhase = ""
		record.Reason = "pre-existing namespace preserved; account and RBAC must be administrator-prepared"
		return record, a.saveEnvironment(record)
	}
	if !ownedNamespace(namespace, record) {
		return record, errors.New("namespace ownership changed; refusing provisioning")
	}
	record.FailedPhase = "service-account"
	accountPath := "/api/v1/namespaces/" + task.Namespace + "/serviceaccounts"
	var account corev1.ServiceAccount
	code, err = a.provisionRequest(ctx, p, "GET", accountPath+"/"+agentprotocol.DeploymentServiceAccount, nil, &account)
	if err != nil {
		return record, err
	}
	if code == 404 {
		automount := false
		desired := &corev1.ServiceAccount{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"}, ObjectMeta: metav1.ObjectMeta{Name: agentprotocol.DeploymentServiceAccount, Namespace: task.Namespace, Annotations: map[string]string{environmentAnnotation: e.ID}}, AutomountServiceAccountToken: &automount}
		code, err = a.provisionRequest(ctx, p, "POST", accountPath, desired, &account)
	}
	if err = requireProvisionStatus(code, err); err != nil {
		return record, err
	}
	if account.UID == "" || account.DeletionTimestamp != nil || account.Annotations[environmentAnnotation] != e.ID || (record.ServiceAccountUID != "" && record.ServiceAccountUID != string(account.UID)) {
		return record, errors.New("deployment account ownership changed; refusing repair")
	}
	record.ServiceAccountUID = string(account.UID)
	record.Resources = []string{"Namespace", "ServiceAccount"}
	if err = a.saveEnvironment(record); err != nil {
		return record, err
	}
	record.FailedPhase = "role-binding"
	rolePath := "/apis/rbac.authorization.k8s.io/v1/namespaces/" + task.Namespace + "/rolebindings"
	var binding rbacv1.RoleBinding
	code, err = a.provisionRequest(ctx, p, "GET", rolePath+"/justcd-deployer", nil, &binding)
	if err != nil {
		return record, err
	}
	desiredRole := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: p.DeploymentCredentials.Provisioning.DeploymentClusterRole}
	desiredSubject := rbacv1.Subject{Kind: "ServiceAccount", Name: agentprotocol.DeploymentServiceAccount, Namespace: task.Namespace}
	if code == 404 {
		desired := &rbacv1.RoleBinding{TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"}, ObjectMeta: metav1.ObjectMeta{Name: "justcd-deployer", Namespace: task.Namespace, Annotations: map[string]string{environmentAnnotation: e.ID}}, RoleRef: desiredRole, Subjects: []rbacv1.Subject{desiredSubject}}
		code, err = a.provisionRequest(ctx, p, "POST", rolePath, desired, &binding)
	}
	if err = requireProvisionStatus(code, err); err != nil {
		return record, err
	}
	if binding.Annotations[environmentAnnotation] != e.ID || binding.RoleRef != desiredRole || len(binding.Subjects) != 1 || binding.Subjects[0] != desiredSubject || binding.DeletionTimestamp != nil {
		return record, errors.New("deployment RoleBinding is not exclusively managed by this environment; refusing modification")
	}
	record.Phase = "ready"
	record.FailedPhase = ""
	record.Reason = ""
	record.Resources = []string{"Namespace", "ServiceAccount", "RoleBinding"}
	return record, a.saveEnvironment(record)
}
func ownedNamespace(namespace corev1.Namespace, r *environmentRecord) bool {
	if r.Platform == "openshift" {
		return namespace.Name == r.Namespace && string(namespace.UID) == r.NamespaceUID
	}
	return string(namespace.UID) == r.NamespaceUID && namespace.Annotations[environmentAnnotation] == r.ID && namespace.Annotations["justcd.io/workspace"] == r.Workspace && namespace.Annotations["justcd.io/application"] == r.OwnerApplication
}
func (a *Agent) releaseEnvironment(ctx context.Context, task agentprotocol.Request, p agentprotocol.Profile, r *environmentRecord) (*environmentRecord, error) {
	e := task.Environment
	if r == nil {
		return nil, errors.New("environment ownership ledger is missing; refusing namespace cleanup")
	}
	if r.Phase == "deleted" {
		return r, nil
	}
	if !r.Managed && r.NamespaceUID == "" && len(r.Resources) == 0 {
		r.Phase = "retained"
		r.Shared = true
		r.Reason = "no managed namespace was established; no namespace deleted"
		return r, a.saveEnvironment(r)
	}
	if r.Phase != "deleting" && (r.Prepared || r.Shared || e.Shared || e.PreserveNamespace || r.OwnerApplication != e.ApplicationID || !p.DeploymentCredentials.Provisioning.DeleteNamespaces) {
		r.Shared = true
		r.Phase = "retained"
		r.Reason = "namespace preserved by ownership or deletion policy"
		return r, a.saveEnvironment(r)
	}
	if r.NamespaceUID == "" || e.ExpectedUID == "" || r.NamespaceUID != e.ExpectedUID {
		return r, errors.New("persisted namespace UID is missing or changed; refusing deletion")
	}
	r.Phase = "deleting"
	r.FailedPhase = "namespace-deletion"
	if err := a.saveEnvironment(r); err != nil {
		return r, err
	}
	var ns corev1.Namespace
	path := "/api/v1/namespaces/" + task.Namespace
	code, err := a.provisionRequest(ctx, p, "GET", path, nil, &ns)
	if err != nil {
		return r, err
	}
	if code == 404 {
		r.Phase = "deleted"
		r.FailedPhase = ""
		r.Reason = ""
		a.credentials.clear()
		return r, a.saveEnvironment(r)
	}
	if err = requireProvisionStatus(code, nil); err != nil {
		return r, err
	}
	if !ownedNamespace(ns, r) {
		return r, errors.New("namespace ownership or UID changed; refusing destructive cleanup")
	}
	if ns.DeletionTimestamp == nil {
		if !p.DeploymentCredentials.Provisioning.DeleteNamespaces {
			return r, errors.New("namespace deletion is disabled by the current local policy; no deletion was issued")
		}
		uid, version := ns.UID, ns.ResourceVersion
		code, err = a.provisionRequest(ctx, p, "DELETE", path, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}, nil)
		if err = requireProvisionStatus(code, err); err != nil {
			return r, err
		}
	}
	r.Reason = "namespace deletion requested; waiting for finalizers and namespace removal"
	a.credentials.clear()
	return r, a.saveEnvironment(r)
}
func (a *Agent) checkEnvironmentWorkload(task agentprotocol.Request, requireInitialized bool) error {
	r, err := a.readEnvironment(task.Namespace)
	if err != nil {
		return err
	}
	if r == nil {
		return nil
	} // Prepared namespaces need no adoption/provisioning.
	if r.Workspace != task.WorkspaceID || r.Profile != task.Profile {
		return errors.New("namespace belongs to another environment policy")
	}
	if r.Phase != "ready" && r.Phase != "retained" {
		return errors.New("namespace environment is not ready; retry provisioning or finish deletion")
	}
	return nil
}
