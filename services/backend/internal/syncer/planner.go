package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Service struct {
	Store         *store.Store
	EncryptionKey []byte
}

type planInput struct {
	Application        store.Application
	Cluster            store.Cluster
	Bindings           []core.Binding
	NamespaceBindings  map[string]store.NamespaceBinding
	NamespaceClients   map[string]*kube.Clients
	ClusterScopeClient *kube.Clients
	Mapper             *kube.Clients
}

func (s *Service) BuildPlan(ctx context.Context, applicationID, actorID string) (store.PlanRecord, error) {
	app, err := s.Store.ApplicationByID(ctx, applicationID)
	if err != nil {
		return store.PlanRecord{}, err
	}
	plan, desired, err := s.CalculatePlan(ctx, app)
	if err != nil {
		return store.PlanRecord{}, err
	}
	now := time.Now().UTC()
	record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: desired, CreatedBy: actorID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Status: "current"}
	if err := s.Store.SavePlan(ctx, record); err != nil {
		return store.PlanRecord{}, err
	}
	if _, err := s.Store.DB.ExecContext(ctx, `UPDATE applications SET health=$2,last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, app.ID, healthForPlan(plan)); err != nil {
		return store.PlanRecord{}, err
	}
	_ = s.Store.Audit(ctx, actorID, "plan.created", "application", app.ID, map[string]any{"planId": record.ID, "revision": plan.Revision, "digest": plan.Digest, "changes": len(plan.Changes)})
	return record, nil
}

func (s *Service) CalculatePlan(ctx context.Context, app store.Application) (core.Plan, []core.Resource, error) {
	source, err := s.Store.GitSourceByID(ctx, app.SourceID)
	if err != nil {
		return core.Plan{}, nil, errors.New("application Git source is unavailable")
	}
	checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, app.Revision)
	if err != nil {
		return core.Plan{}, nil, err
	}
	defer checkout.Close()
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Plan{}, nil, err
	}
	namespaceBindings := make([]core.Binding, 0, len(input.Bindings))
	for _, binding := range input.Bindings {
		if !binding.ClusterScope {
			namespaceBindings = append(namespaceBindings, binding)
		}
	}
	desired, err := render.Render(ctx, render.Options{RepositoryRoot: checkout.Root, ManifestPath: app.ManifestPath, Renderer: app.Renderer, ApplicationID: app.ID, ClusterID: app.ClusterID, Namespaces: namespaceBindings, Mapper: input.Mapper.Mapper})
	if err != nil {
		return core.Plan{}, nil, err
	}
	for _, resource := range desired {
		if resource.Identity.ClusterScoped && input.ClusterScopeClient == nil {
			return core.Plan{}, nil, fmt.Errorf("cluster-scoped resource %s/%s requires a privileged cluster-scope credential", resource.Identity.Kind, resource.Identity.Name)
		}
	}
	live, err := s.liveSnapshot(ctx, input, desired)
	if err != nil {
		return core.Plan{}, nil, err
	}
	plan, err := core.BuildPlan(app.ID, checkout.Commit, input.Bindings, desired, live)
	if err != nil {
		return core.Plan{}, nil, err
	}
	return plan, desired, nil
}

func healthForPlan(plan core.Plan) string {
	if plan.RequiresApproval {
		for _, change := range plan.Changes {
			if change.Kind == core.Delete {
				return "deletion_pending"
			}
		}
	}
	if len(plan.Changes) > 0 {
		return "out_of_sync"
	}
	return "synced"
}

func (s *Service) loadPlanInput(ctx context.Context, app store.Application) (planInput, error) {
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return planInput{}, errors.New("application cluster is unavailable")
	}
	input := planInput{Application: app, Cluster: cluster, NamespaceBindings: map[string]store.NamespaceBinding{}, NamespaceClients: map[string]*kube.Clients{}}
	if len(app.Namespaces) == 0 {
		return planInput{}, errors.New("application has no namespace bindings")
	}
	for _, binding := range app.Namespaces {
		stored, err := s.Store.NamespaceBinding(ctx, app.ProjectID, app.ClusterID, binding.Namespace)
		if err != nil {
			return planInput{}, fmt.Errorf("namespace %q is no longer bound to this project", binding.Namespace)
		}
		credentialID := stored.CredentialID
		if credentialID == nil {
			credentialID = cluster.DefaultCredentialID
		}
		if credentialID == nil {
			return planInput{}, fmt.Errorf("namespace %q has no Kubernetes credential", binding.Namespace)
		}
		input.Bindings = append(input.Bindings, core.Binding{ClusterID: cluster.ID, Namespace: stored.Namespace, CredentialRef: *credentialID})
		client, err := kube.ForBinding(ctx, s.Store, s.EncryptionKey, cluster, stored.CredentialID, false)
		if err != nil {
			return planInput{}, err
		}
		input.NamespaceBindings[stored.Namespace] = stored
		input.NamespaceClients[stored.Namespace] = client
	}
	if cluster.ClusterScopeCredential != nil {
		input.Bindings = append(input.Bindings, core.Binding{ClusterID: cluster.ID, CredentialRef: *cluster.ClusterScopeCredential, ClusterScope: true})
		input.ClusterScopeClient, err = kube.ForBinding(ctx, s.Store, s.EncryptionKey, cluster, cluster.ClusterScopeCredential, true)
		if err != nil {
			return planInput{}, err
		}
	}
	input.Mapper = input.ClusterScopeClient
	if input.Mapper == nil {
		for _, client := range input.NamespaceClients {
			input.Mapper = client
			break
		}
	}
	if input.Mapper == nil {
		return planInput{}, errors.New("application has no Kubernetes discovery client")
	}
	_, err = input.Mapper.Discovery.ServerVersion()
	if err != nil {
		return planInput{}, fmt.Errorf("Kubernetes API discovery failed: %w", err)
	}
	return input, nil
}

func (s *Service) liveSnapshot(ctx context.Context, input planInput, desired []core.Resource) ([]core.Resource, error) {
	managed, err := s.Store.ManagedResources(ctx, input.Application.ID)
	if err != nil {
		return nil, err
	}
	identities := map[string]core.Identity{}
	desiredIndex := map[string]int{}
	managedByKey := map[string]store.ManagedResource{}
	for index, resource := range desired {
		identities[resource.Identity.Key()] = resource.Identity
		desiredIndex[resource.Identity.Key()] = index
	}
	previousManifests := map[string][]byte{}
	for _, resource := range managed {
		identity := resource.Identity
		identity.ClusterScoped = identity.Namespace == ""
		identities[identity.Key()] = identity
		managedByKey[identity.Key()] = resource
		previousManifests[identity.Key()] = resource.Manifest
	}
	keys := make([]string, 0, len(identities))
	for key := range identities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	liveByKey := map[string]core.Resource{}
	for _, key := range keys {
		identity := identities[key]
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(identity), groupVersion(identity))
		if err != nil {
			return nil, fmt.Errorf("resource kind %s %s is not discoverable: %w", identity.APIVersion, identity.Kind, err)
		}
		if mapping.Scope.Name() == "namespace" && identity.ClusterScoped {
			return nil, errors.New("Kubernetes discovery scope changed for a managed resource")
		}
		if mapping.Scope.Name() != "namespace" && !identity.ClusterScoped {
			return nil, errors.New("Kubernetes discovery scope changed for a managed resource")
		}
		client, err := clientFor(input, identity)
		if err != nil {
			return nil, err
		}
		var resourceInterface dynamic.ResourceInterface
		if mapping.Scope.Name() == "namespace" {
			resourceInterface = client.Dynamic.Resource(mapping.Resource).Namespace(identity.Namespace)
		} else {
			resourceInterface = client.Dynamic.Resource(mapping.Resource)
		}
		object, err := resourceInterface.Get(ctx, identity.Name, v1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if _, tracked := managedByKey[identity.Key()]; tracked {
				if err := s.Store.DeleteManagedResource(ctx, input.Application.ID, identity); err != nil {
					return nil, err
				}
				delete(managedByKey, identity.Key())
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s %s/%s: %w", identity.Kind, identity.Namespace, identity.Name, err)
		}
		tracked, owned := managedByKey[identity.Key()]
		if !owned {
			return nil, fmt.Errorf("%s %s/%s exists but is not recorded as managed by this application", identity.Kind, identity.Namespace, identity.Name)
		}
		if object.GetLabels()["justcd.io/application-id"] != input.Application.ID || string(object.GetUID()) != tracked.UID {
			return nil, fmt.Errorf("%s %s/%s ownership changed; refusing to adopt or mutate it", identity.Kind, identity.Namespace, identity.Name)
		}
		var resource core.Resource
		if index, exists := desiredIndex[identity.Key()]; exists {
			var desiredFingerprint string
			resource, desiredFingerprint, err = render.CanonicalLiveAgainst(object, input.Cluster.ID, input.Application.ID, identity.ClusterScoped, desired[index].Manifest, previousManifests[identity.Key()])
			if err == nil {
				desired[index].Fingerprint = desiredFingerprint
			}
		} else {
			resource, err = render.CanonicalLive(object, input.Cluster.ID, input.Application.ID, identity.ClusterScoped)
		}
		if err != nil {
			return nil, err
		}
		liveByKey[identity.Key()] = resource
	}
	kinds := map[string]core.Identity{}
	for _, identity := range identities {
		kinds[kindKey(identity)] = identity
	}
	kindKeys := make([]string, 0, len(kinds))
	for key := range kinds {
		kindKeys = append(kindKeys, key)
	}
	sort.Strings(kindKeys)
	for _, key := range kindKeys {
		identity := kinds[key]
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(identity), groupVersion(identity))
		if err != nil {
			return nil, err
		}
		namespaces := []string{identity.Namespace}
		if mapping.Scope.Name() == "namespace" {
			namespaces = namespaces[:0]
			for namespace := range input.NamespaceClients {
				namespaces = append(namespaces, namespace)
			}
			sort.Strings(namespaces)
		} else {
			if input.ClusterScopeClient == nil {
				return nil, errors.New("cluster-scoped live discovery requires a privileged cluster-scope credential")
			}
			namespaces = []string{""}
		}
		for _, namespace := range namespaces {
			id := identity
			id.Namespace = namespace
			id.ClusterScoped = mapping.Scope.Name() != "namespace"
			client, err := clientFor(input, id)
			if err != nil {
				return nil, err
			}
			var resourceInterface dynamic.ResourceInterface
			if mapping.Scope.Name() == "namespace" {
				resourceInterface = client.Dynamic.Resource(mapping.Resource).Namespace(namespace)
			} else {
				resourceInterface = client.Dynamic.Resource(mapping.Resource)
			}
			continueToken := ""
			for page := 0; page < 100; page++ {
				list, err := resourceInterface.List(ctx, v1.ListOptions{LabelSelector: "justcd.io/application-id=" + input.Application.ID, Limit: 500, Continue: continueToken})
				if err != nil {
					return nil, fmt.Errorf("cannot fully list owned %s resources: %w", identity.Kind, err)
				}
				for i := range list.Items {
					object := &list.Items[i]
					objectIdentity := core.Identity{ClusterID: input.Cluster.ID, APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), ClusterScoped: id.ClusterScoped}
					tracked, owned := managedByKey[objectIdentity.Key()]
					if !owned {
						continue
					}
					if string(object.GetUID()) != tracked.UID {
						return nil, fmt.Errorf("%s %s/%s ownership changed; refusing to adopt or mutate it", objectIdentity.Kind, objectIdentity.Namespace, objectIdentity.Name)
					}
					var resource core.Resource
					if desiredIndexFor, exists := desiredIndex[objectIdentity.Key()]; exists {
						var desiredFingerprint string
						resource, desiredFingerprint, err = render.CanonicalLiveAgainst(object, input.Cluster.ID, input.Application.ID, id.ClusterScoped, desired[desiredIndexFor].Manifest, previousManifests[desired[desiredIndexFor].Identity.Key()])
						if err == nil {
							desired[desiredIndexFor].Fingerprint = desiredFingerprint
						}
					} else {
						resource, err = render.CanonicalLive(object, input.Cluster.ID, input.Application.ID, id.ClusterScoped)
					}
					if err != nil {
						return nil, err
					}
					liveByKey[resource.Identity.Key()] = resource
					if len(liveByKey) > maxSnapshotResources {
						return nil, errors.New("application resource snapshot exceeds 10000 resources")
					}
				}
				continueToken = list.GetContinue()
				if continueToken == "" {
					break
				}
				if page == 99 {
					return nil, errors.New("resource listing exceeded page limit")
				}
			}
		}
	}
	result := make([]core.Resource, 0, len(liveByKey))
	for _, resource := range liveByKey {
		result = append(result, resource)
	}
	return result, nil
}

const maxSnapshotResources = 10000

func clientFor(input planInput, identity core.Identity) (*kube.Clients, error) {
	if identity.ClusterScoped {
		if input.ClusterScopeClient == nil {
			return nil, errors.New("cluster-scoped access is not configured")
		}
		return input.ClusterScopeClient, nil
	}
	client := input.NamespaceClients[identity.Namespace]
	if client == nil {
		return nil, fmt.Errorf("namespace %q is not bound", identity.Namespace)
	}
	return client, nil
}
func groupVersion(identity core.Identity) string { return identity.APIVersion }
func groupKind(identity core.Identity) schema.GroupKind {
	gv, _ := schema.ParseGroupVersion(identity.APIVersion)
	return schema.GroupKind{Group: gv.Group, Kind: identity.Kind}
}
func kindKey(identity core.Identity) string {
	return strings.Join([]string{identity.APIVersion, identity.Kind, fmt.Sprint(identity.ClusterScoped)}, "\x00")
}
