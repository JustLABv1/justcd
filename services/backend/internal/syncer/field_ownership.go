package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

func fieldOwnershipConflict(err error) bool {
	var status apierrors.APIStatus
	if !errors.As(err, &status) || !apierrors.IsConflict(err) {
		return false
	}
	details := status.Status().Details
	if details == nil || len(details.Causes) == 0 {
		return false
	}
	for _, cause := range details.Causes {
		if cause.Type != metav1.CauseTypeFieldManagerConflict {
			return false
		}
	}
	return true
}

// Detect field ownership transfers before sealing the plan and its approval
// policy. This includes Apply/Update ownership under the same manager name.
// All requests are dry-runs; only an approved takeover can force a real apply.
func (s *Service) detectFieldTakeovers(ctx context.Context, input planInput, plan *core.Plan, desired []core.Resource) error {
	byKey := make(map[string]core.Resource, len(desired))
	for _, resource := range desired {
		byKey[resource.Identity.Key()] = resource
	}
	for index := range plan.Changes {
		change := &plan.Changes[index]
		if change.Kind != core.Update {
			continue
		}
		resource, ok := byKey[change.Identity.Key()]
		if !ok {
			return errors.New("update plan desired resource is missing")
		}
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(change.Identity), groupVersion(change.Identity))
		if err != nil {
			return err
		}
		client, err := clientFor(input, change.Identity)
		if err != nil {
			return err
		}
		var rc dynamic.ResourceInterface = client.Dynamic.Resource(mapping.Resource)
		if mapping.Scope.Name() == "namespace" {
			rc = client.Dynamic.Resource(mapping.Resource).Namespace(change.Identity.Namespace)
		}
		object, err := render.NormalizedObject(resource)
		if err != nil {
			return err
		}
		object.SetLabels(mergeOwnerLabel(object.GetLabels(), plan.ApplicationID))
		object.SetResourceVersion(change.LiveResourceVersion)
		body, err := object.MarshalJSON()
		if err != nil {
			return err
		}
		options := applyPatchOptions(*change, "justcd/"+plan.ApplicationID)
		options.DryRun = []string{metav1.DryRunAll}
		_, err = rc.Patch(ctx, change.Identity.Name, types.ApplyPatchType, body, options)
		if !change.Takeover && fieldOwnershipConflict(err) {
			change.Takeover = true
			options = applyPatchOptions(*change, "justcd/"+plan.ApplicationID)
			options.DryRun = []string{metav1.DryRunAll}
			_, err = rc.Patch(ctx, change.Identity.Name, types.ApplyPatchType, body, options)
		}
		if err != nil {
			return fmt.Errorf("update dry-run failed for %s %s/%s: %w", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, err)
		}
	}
	return nil
}
