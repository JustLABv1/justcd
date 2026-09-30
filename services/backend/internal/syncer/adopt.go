package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var ErrAdoptionStale = errors.New("resource or Git changed; refresh the ownership conflict")
var ErrAdoptionUnsafe = errors.New("resource cannot be safely adopted")

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
	return s.adoptValidatedConflict(ctx, input, app, actorID, reason, *conflict, expected)
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
		_, err := s.adoptValidatedConflict(ctx, input, app, actorID, reason, *byKey[selected.Identity.Key()], selected)
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

func (s *Service) adoptValidatedConflict(ctx context.Context, input planInput, app store.Application, actorID, reason string, conflict OwnershipConflict, expected OwnershipConflict) (core.Resource, error) {
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
	object, err := resourceClient.Get(ctx, conflict.Identity.Name, metav1.GetOptions{})
	if err != nil || string(object.GetUID()) != expected.UID || object.GetResourceVersion() != expected.ResourceVersion {
		return core.Resource{}, ErrAdoptionStale
	}
	owner := object.GetLabels()["justcd.io/application-id"]
	if owner != expected.Owner {
		return core.Resource{}, ErrAdoptionStale
	}
	ownerMissing, err := s.adoptionOwnerMissing(ctx, owner, app.ID)
	if err != nil {
		return core.Resource{}, err
	}
	if owner != "" && owner != app.ID && !ownerMissing {
		return core.Resource{}, fmt.Errorf("%w: ownership label belongs to an existing application", ErrAdoptionUnsafe)
	}
	if len(object.GetOwnerReferences()) != 0 {
		return core.Resource{}, fmt.Errorf("%w: object has Kubernetes owner references", ErrAdoptionUnsafe)
	}
	if object.GetLabels()["justcd.io/application-id"] != app.ID {
		operations := []map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": expected.UID},
			{"op": "test", "path": "/metadata/resourceVersion", "value": expected.ResourceVersion},
		}
		if object.GetLabels() == nil {
			operations = append(operations, map[string]any{"op": "add", "path": "/metadata/labels", "value": map[string]string{"justcd.io/application-id": app.ID}})
		} else {
			operations = append(operations, map[string]any{"op": "add", "path": "/metadata/labels/justcd.io~1application-id", "value": app.ID})
		}
		patch, err := json.Marshal(operations)
		if err != nil {
			return core.Resource{}, err
		}
		object, err = resourceClient.Patch(ctx, conflict.Identity.Name, types.JSONPatchType, patch, metav1.PatchOptions{FieldManager: "justcd-adoption/" + app.ID})
		if err != nil {
			return core.Resource{}, fmt.Errorf("%w: label claim was rejected: %v", ErrAdoptionStale, err)
		}
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

func validateAdoptionConflict(fresh, expected *OwnershipConflict, applicationID string) error {
	if fresh == nil || expected == nil || fresh.Identity.Key() != expected.Identity.Key() || fresh.UID != expected.UID || fresh.ResourceVersion != expected.ResourceVersion || fresh.DesiredFingerprint != expected.DesiredFingerprint {
		return ErrAdoptionStale
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
