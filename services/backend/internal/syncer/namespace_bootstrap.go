package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var projectResource = schema.GroupVersionResource{Group: "project.openshift.io", Version: "v1", Resource: "projects"}
var projectRequestResource = schema.GroupVersionResource{Group: "project.openshift.io", Version: "v1", Resource: "projectrequests"}

// BootstrapRepositoryPRNamespaces provisions the owner-authorized dev environment
// before live workload planning. The namespace/project is shared infrastructure,
// not an application-owned resource, and remains after an unsuccessful plan.
func (s *Service) BootstrapRepositoryPRNamespaces(ctx context.Context, applicationID string) error {
	app, err := s.Store.ApplicationByID(ctx, applicationID)
	if err != nil || !app.CreateNamespaces {
		return err
	}
	review, connection, err := s.Store.ReviewByPreviewApplication(ctx, app.ID)
	if err != nil || connection.RepositoryPRID == nil || !connection.Enabled || connection.WorkspaceID != app.WorkspaceID || connection.ApplicationID != app.ID {
		return errors.New("automatic PR namespace setup requires a registered repository PR application")
	}
	entry, err := s.Store.RepositoryPRByID(ctx, *connection.RepositoryPRID)
	if err != nil {
		return err
	}
	repo, err := s.Store.RepositoryConfigurationByID(ctx, entry.RepositoryID)
	if err != nil {
		return err
	}
	if !repositoryPRNamespaceBootstrapAllowed(app, repo, entry, review) {
		return errors.New("repository PR policy or current PR state does not authorize namespace setup")
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return err
	}
	namespace := app.Namespaces[0].Namespace
	created, err := bootstrapNamespace(ctx, input, namespace)
	if err != nil {
		return fmt.Errorf("bootstrap namespace %q: %w", namespace, err)
	}
	if created {
		_ = s.Store.Audit(ctx, "", "repository.pr_namespace.created", "application", app.ID, map[string]any{"namespace": namespace, "clusterId": app.ClusterID, "repositoryId": repo.ID, "pullRequest": entry.Number})
	}
	return nil
}

func repositoryPRNamespaceBootstrapAllowed(app store.Application, repo store.RepositoryConfiguration, entry store.RepositoryPRApplication, review store.PullRequestReview) bool {
	return app.CreateNamespaces && len(app.Namespaces) == 1 && repo.Enabled && repo.PRSettings.AllowsNamespaceCreation(app.ClusterID, app.Namespaces[0].Namespace) &&
		entry.RepositoryID == repo.ID && entry.Mode == "existing" && entry.ApplicationID != nil && *entry.ApplicationID == app.ID && !entry.DefinitionRemoved && !entry.Merged && entry.Phase != "promoted" &&
		app.WorkspaceID == repo.WorkspaceID && app.SourceID == repo.SourceID && app.Revision != "" && app.Revision == entry.HeadSHA && app.Revision == review.HeadSHA &&
		review.Number == entry.Number && !review.Closed && !review.Fork
}

func bootstrapNamespace(ctx context.Context, input planInput, name string) (bool, error) {
	if !input.Application.CreateNamespaces {
		return false, errors.New("namespace creation is disabled")
	}
	if _, ok := input.NamespaceBindings[name]; !ok {
		return false, fmt.Errorf("namespace %q is not bound to this workspace", name)
	}
	// Use the workload credential for OpenShift so project-admin belongs to the
	// account that will plan and deploy, not a separate cluster-scope account.
	client := input.NamespaceClients[name]
	if client == nil || client.Mapper == nil {
		return false, errors.New("namespace bootstrap requires a Kubernetes discovery client")
	}
	_, err := client.Mapper.RESTMapping(schema.GroupKind{Group: projectRequestResource.Group, Kind: "ProjectRequest"}, projectRequestResource.Version)
	if err == nil {
		project, getErr := client.Dynamic.Resource(projectResource).Get(ctx, name, metav1.GetOptions{})
		if getErr == nil {
			return false, checkBootstrapNamespace(project, name)
		}
		// OpenShift may hide an absent project behind Forbidden. Requesting the
		// project is safe: its API rejects existing projects without changing RBAC.
		if !apierrors.IsNotFound(getErr) && !apierrors.IsForbidden(getErr) {
			return false, getErr
		}
		request := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "project.openshift.io/v1", "kind": "ProjectRequest", "metadata": map[string]any{"name": name}}}
		_, createErr := client.Dynamic.Resource(projectRequestResource).Create(ctx, request, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			project, readErr := client.Dynamic.Resource(projectResource).Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsForbidden(readErr) {
				// AlreadyExists proves the environment is present. Project-read
				// access is not a prerequisite for workload access. Do not repair
				// ownership/RBAC; let normal workload planning check permissions.
				return false, nil
			}
			if readErr != nil {
				return false, readErr
			}
			return false, checkBootstrapNamespace(project, name)
		}
		// Never bypass a denied ProjectRequest by creating a bare Namespace.
		return createErr == nil, createErr
	}
	if !meta.IsNoMatchError(err) {
		return false, err
	}
	provisioningClient, err := namespaceProvisioningClient(input, name)
	if err != nil {
		return false, err
	}
	resource := provisioningClient.Dynamic.Resource(namespaceResource)
	existing, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return false, checkBootstrapNamespace(existing, name)
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	_, err = resource.Create(ctx, namespaceObject(name), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, readErr := resource.Get(ctx, name, metav1.GetOptions{})
		if readErr != nil {
			return false, readErr
		}
		return false, checkBootstrapNamespace(existing, name)
	}
	return err == nil, err
}

func checkBootstrapNamespace(object *unstructured.Unstructured, name string) error {
	if object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("namespace %q is terminating", name)
	}
	return nil
}
