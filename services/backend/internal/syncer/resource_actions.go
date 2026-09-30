package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type ResourceAction struct {
	Identity core.Identity `json:"identity"`
	UID      string        `json:"uid"`
	Action   string        `json:"action"`
	Replicas *int64        `json:"replicas,omitempty"`
}

func validateResourceAction(input ResourceAction) error {
	if input.Identity.Namespace == "" || input.Identity.ClusterScoped {
		return errors.New("actions require a namespaced managed resource")
	}
	switch input.Action {
	case "delete":
	case "rescale", "redeploy":
		if input.Identity.APIVersion != "apps/v1" || (input.Identity.Kind != "Deployment" && input.Identity.Kind != "StatefulSet" && !(input.Action == "redeploy" && input.Identity.Kind == "DaemonSet")) {
			return errors.New("action is not supported for this workload")
		}
		if input.Action == "rescale" && (input.Replicas == nil || *input.Replicas < 0 || *input.Replicas > 10000) {
			return errors.New("replicas must be between 0 and 10000")
		}
	default:
		return errors.New("action must be rescale, redeploy, or delete")
	}
	if input.UID == "" {
		return errors.New("resource UID is required")
	}
	return nil
}

func (s *Service) ResourceAction(ctx context.Context, app store.Application, actorID string, action ResourceAction) (err error) {
	if err := validateResourceAction(action); err != nil {
		return err
	}
	if app.Decommissioning {
		return errors.New("application is being decommissioned")
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return err
	}
	found := false
	for _, resource := range managed {
		if resource.Identity.Key() == action.Identity.Key() && resource.UID == action.UID {
			found = true
			break
		}
	}
	if !found {
		return errors.New("managed resource changed; refresh the resource inventory")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	operationID, err := s.Store.AcquireResourceAction(ctx, app.ID, actorID, 90*time.Second)
	if err != nil {
		return err
	}
	defer func() {
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer finishCancel()
		status, message := "succeeded", "Resource "+action.Action+" completed; reconciliation remains paused"
		if err != nil {
			status, message = "failed", "Resource action failed; reconciliation remains paused"
		}
		_ = s.Store.FinishOperation(finishCtx, operationID, status, message)
		_ = s.Store.Audit(finishCtx, actorID, "resource."+action.Action, "application", app.ID, map[string]any{"identity": action.Identity, "replicas": action.Replicas, "status": status, "operationId": operationID})
	}()
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return err
	}
	mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(action.Identity), groupVersion(action.Identity))
	if err != nil {
		return err
	}
	client, err := clientFor(input, action.Identity)
	if err != nil {
		return err
	}
	var resourceClient dynamic.ResourceInterface = client.Dynamic.Resource(mapping.Resource).Namespace(action.Identity.Namespace)
	object, err := resourceClient.Get(ctx, action.Identity.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(object.GetUID()) != action.UID || object.GetLabels()["justcd.io/application-id"] != app.ID {
		return errors.New("resource identity or ownership changed; refresh before retrying")
	}
	if action.Action == "delete" {
		uid, version := object.GetUID(), object.GetResourceVersion()
		policy := metav1.DeletePropagationForeground
		return resourceClient.Delete(ctx, action.Identity.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}, PropagationPolicy: &policy})
	}
	patch := map[string]any{"metadata": map[string]any{"uid": action.UID, "resourceVersion": object.GetResourceVersion()}}
	if action.Action == "rescale" {
		patch["spec"] = map[string]any{"replicas": *action.Replicas}
	} else {
		patch["spec"] = map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano)}}}}
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("encode resource action: %w", err)
	}
	_, err = resourceClient.Patch(ctx, action.Identity.Name, types.MergePatchType, raw, metav1.PatchOptions{})
	return err
}
