package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var ErrAdoptionStale = errors.New("resource or Git changed; refresh the ownership conflict")
var ErrAdoptionUnsafe = errors.New("resource cannot be safely adopted")

// Adoption claims only a label, so bind its review to the source inputs rather
// than generated output (Helm charts can generate random secrets/checksums).
// Workload changes still require their own freshly reviewed apply plan.
func adoptionReviewFingerprint(source store.GitSource, revision, commit string, options render.Options) (string, error) {
	// These vary between checkouts/discovery and are not renderer configuration.
	options.RepositoryRoot = ""
	options.Mapper = nil
	data, err := json.Marshal(struct {
		SourceID      string
		RepositoryURL string
		Revision      string
		Commit        string
		Options       render.Options
	}{source.ID, source.RepositoryURL, revision, commit, options})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "source-v1:" + hex.EncodeToString(sum[:]), nil
}

// AdoptResource claims only the JustCD ownership label. It never applies the
// desired workload manifest. A fresh, manual plan is required for that step.
func (s *Service) AdoptResource(ctx context.Context, app store.Application, actorID, reason string, expected OwnershipConflict) (core.Resource, error) {
	conflicts, err := s.OwnershipConflicts(ctx, app)
	if err != nil {
		return core.Resource{}, err
	}
	var conflict *OwnershipConflict
	for _, candidate := range conflicts {
		if candidate.Identity.Key() == expected.Identity.Key() {
			conflict = candidate
			break
		}
	}
	if err := validateAdoptionConflict(conflict, &expected, app.ID); err != nil {
		return core.Resource{}, err
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Resource{}, err
	}
	return s.adoptValidatedConflict(ctx, input, app, actorID, reason, *conflict)
}

func (s *Service) OwnershipConflicts(ctx context.Context, app store.Application) ([]*OwnershipConflict, error) {
	_, _, err := s.CalculatePlan(ctx, app)
	if err == nil {
		return []*OwnershipConflict{}, nil
	}
	var conflicts *OwnershipConflicts
	if errors.As(err, &conflicts) {
		return conflicts.Items, nil
	}
	return nil, err
}

type AdoptionResult struct {
	Identity core.Identity `json:"identity"`
	Status   string        `json:"status"`
	Error    string        `json:"error,omitempty"`
}

// AdoptResources validates the complete review set before claiming anything.
// Each claim still has its own UID/resource-version precondition, so an object
// changing mid-batch fails individually without undoing earlier claims.
func (s *Service) AdoptResources(ctx context.Context, app store.Application, actorID, reason string, expected []OwnershipConflict) ([]AdoptionResult, error) {
	if len(expected) == 0 || len(expected) > 200 {
		return nil, errors.New("select between 1 and 200 resources")
	}
	fresh, err := s.OwnershipConflicts(ctx, app)
	if err != nil {
		return nil, err
	}
	byKey, err := validateAdoptionSelection(fresh, expected, app.ID)
	if err != nil {
		return nil, err
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return nil, err
	}
	results := make([]AdoptionResult, 0, len(expected))
	for _, selected := range expected {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		_, err := s.adoptValidatedConflict(ctx, input, app, actorID, reason, *byKey[selected.Identity.Key()])
		item := AdoptionResult{Identity: selected.Identity, Status: "claimed"}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
		}
		results = append(results, item)
	}
	return results, nil
}

func validateAdoptionSelection(fresh []*OwnershipConflict, expected []OwnershipConflict, applicationID string) (map[string]*OwnershipConflict, error) {
	byKey := make(map[string]*OwnershipConflict, len(fresh))
	for _, conflict := range fresh {
		byKey[conflict.Identity.Key()] = conflict
	}
	seen := make(map[string]bool, len(expected))
	for _, selected := range expected {
		key := selected.Identity.Key()
		if seen[key] {
			return nil, errors.New("duplicate resource selection")
		}
		seen[key] = true
		if err := validateAdoptionConflict(byKey[key], &selected, applicationID); err != nil {
			return nil, err
		}
	}
	return byKey, nil
}

func (s *Service) adoptValidatedConflict(ctx context.Context, input planInput, app store.Application, actorID, reason string, conflict OwnershipConflict) (core.Resource, error) {
	mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(conflict.Identity), groupVersion(conflict.Identity))
	if err != nil {
		return core.Resource{}, err
	}
	client, err := clientFor(input, conflict.Identity)
	if err != nil {
		return core.Resource{}, err
	}
	var resourceClient dynamic.ResourceInterface
	if conflict.Identity.ClusterScoped {
		resourceClient = client.Dynamic.Resource(mapping.Resource)
	} else {
		resourceClient = client.Dynamic.Resource(mapping.Resource).Namespace(conflict.Identity.Namespace)
	}
	owner := conflict.Owner
	ownerMissing, err := s.adoptionOwnerMissing(ctx, owner, app.ID)
	if err != nil {
		return core.Resource{}, err
	}
	if owner != "" && owner != app.ID && !ownerMissing {
		return core.Resource{}, fmt.Errorf("%w: ownership label belongs to an existing application", ErrAdoptionUnsafe)
	}
	object, err := claimAdoptionLabel(ctx, resourceClient, app.ID, conflict)
	if err != nil {
		return core.Resource{}, err
	}
	current, err := render.CanonicalLive(object, input.Cluster.ID, app.ID, conflict.Identity.ClusterScoped)
	if err != nil {
		return core.Resource{}, err
	}
	current.Manifest = conflict.DesiredManifest
	if err := s.Store.AdoptManagedResource(ctx, app.ID, actorID, reason, current); err != nil {
		// A retry is safe: a same-application label without an inventory row is
		// still reported as a conflict and can be claimed again.
		return core.Resource{}, err
	}
	return current, nil
}

// Re-read and revalidate on every attempt. Only status/server metadata churn may
// advance the resource version; a changed configuration still needs review.
func claimAdoptionLabel(ctx context.Context, resourceClient dynamic.ResourceInterface, applicationID string, conflict OwnershipConflict) (*unstructured.Unstructured, error) {
	var claimError error
	var failedVersion string
	for attempt := 0; attempt < 3; attempt++ {
		object, err := resourceClient.Get(ctx, conflict.Identity.Name, metav1.GetOptions{})
		if err != nil {
			return nil, &resourceAccessError{identity: conflict.Identity, action: "get", err: err}
		}
		if err := validateAdoptionObject(object, conflict, applicationID); err != nil {
			return nil, err
		}
		if claimError != nil && object.GetResourceVersion() == failedVersion {
			// An unchanged object means this was not a resource-version race
			// (for example, an admission rejection). Preserve the actual cause.
			return nil, &resourceAccessError{identity: conflict.Identity, action: "claim ownership label", err: claimError}
		}
		if object.GetLabels()["justcd.io/application-id"] == applicationID {
			return object, nil
		}
		operations := []map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": conflict.UID},
			{"op": "test", "path": "/metadata/resourceVersion", "value": object.GetResourceVersion()},
		}
		if object.GetLabels() == nil {
			operations = append(operations, map[string]any{"op": "add", "path": "/metadata/labels", "value": map[string]string{"justcd.io/application-id": applicationID}})
		} else {
			operations = append(operations, map[string]any{"op": "add", "path": "/metadata/labels/justcd.io~1application-id", "value": applicationID})
		}
		patch, err := json.Marshal(operations)
		if err != nil {
			return nil, err
		}
		claimed, err := resourceClient.Patch(ctx, conflict.Identity.Name, types.JSONPatchType, patch, metav1.PatchOptions{FieldManager: "justcd-adoption/" + applicationID})
		if err == nil {
			return claimed, nil
		}
		if !apierrors.IsConflict(err) && !apierrors.IsInvalid(err) {
			return nil, &resourceAccessError{identity: conflict.Identity, action: "claim ownership label", err: err}
		}
		claimError, failedVersion = err, object.GetResourceVersion()
	}
	return nil, fmt.Errorf("%w: %s %s/%s kept changing while claiming its ownership label", ErrAdoptionStale, conflict.Identity.Kind, conflict.Identity.Namespace, conflict.Identity.Name)
}

func validateAdoptionObject(object *unstructured.Unstructured, conflict OwnershipConflict, applicationID string) error {
	if string(object.GetUID()) != conflict.UID || object.GetLabels()["justcd.io/application-id"] != conflict.Owner {
		return fmt.Errorf("%w: resource identity or ownership changed", ErrAdoptionStale)
	}
	if len(object.GetOwnerReferences()) != 0 {
		return fmt.Errorf("%w: object has Kubernetes owner references", ErrAdoptionUnsafe)
	}
	current, err := render.CanonicalLive(object, conflict.Identity.ClusterID, applicationID, conflict.Identity.ClusterScoped)
	if err != nil {
		return err
	}
	return validateAdoptionLiveVersion(current.Fingerprint, current.ResourceVersion, conflict)
}

func validateAdoptionLiveVersion(fingerprint, resourceVersion string, expected OwnershipConflict) error {
	target := expected.Identity.Kind + " " + expected.Identity.Namespace + "/" + expected.Identity.Name
	if fingerprint != "" && expected.LiveFingerprint != "" {
		if fingerprint != expected.LiveFingerprint {
			return fmt.Errorf("%w: the live configuration for %s changed", ErrAdoptionStale, target)
		}
	} else if resourceVersion != expected.ResourceVersion {
		// Older clients have no configuration fingerprint and must refresh if
		// the resource version changed; never relax their review implicitly.
		return fmt.Errorf("%w: %s changed in Kubernetes (resource version %s -> %s); refresh the ownership review", ErrAdoptionStale, target, expected.ResourceVersion, resourceVersion)
	}
	return nil
}

func validateAdoptionConflict(fresh, expected *OwnershipConflict, applicationID string) error {
	if fresh == nil || expected == nil || fresh.Identity.Key() != expected.Identity.Key() {
		return fmt.Errorf("%w: the selected resource is no longer in the conflict list", ErrAdoptionStale)
	}
	target := fresh.Identity.Kind + " " + fresh.Identity.Namespace + "/" + fresh.Identity.Name
	if fresh.UID != expected.UID {
		return fmt.Errorf("%w: %s was replaced in Kubernetes", ErrAdoptionStale, target)
	}
	if err := validateAdoptionLiveVersion(fresh.LiveFingerprint, fresh.ResourceVersion, *expected); err != nil {
		return err
	}
	if fresh.ReviewFingerprint != "" {
		if fresh.ReviewFingerprint != expected.ReviewFingerprint {
			return fmt.Errorf("%w: the Git revision or renderer configuration for %s changed; refresh the ownership review", ErrAdoptionStale, target)
		}
	} else if fresh.DesiredFingerprint != expected.DesiredFingerprint {
		return fmt.Errorf("%w: the rendered Git manifest for %s changed", ErrAdoptionStale, target)
	}
	if fresh.Owner != "" && fresh.Owner != applicationID && !fresh.OwnerMissing {
		return fmt.Errorf("%w: this object is labelled for another JustCD application", ErrAdoptionUnsafe)
	}
	if fresh.Owner != expected.Owner {
		return ErrAdoptionStale
	}
	if fresh.HasOwnerReferences {
		return fmt.Errorf("%w: this object is a dependent of another Kubernetes resource", ErrAdoptionUnsafe)
	}
	return nil
}

func (s *Service) adoptionOwnerMissing(ctx context.Context, owner, applicationID string) (bool, error) {
	if owner == "" || owner == applicationID {
		return false, nil
	}
	exists, err := s.Store.ApplicationExists(ctx, owner)
	if err != nil {
		return false, err
	}
	return !exists, nil
}
