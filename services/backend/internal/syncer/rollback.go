package syncer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type rollbackSnapshotPayload struct {
	Version          int                   `json:"version"`
	Settings         core.RollbackSettings `json:"settings"`
	ResolvedRevision string                `json:"resolvedRevision,omitempty"`
	OperationID      string                `json:"operationId,omitempty"`
	Resources        []core.Resource       `json:"resources"`
}

func rollbackSettings(app store.Application) core.RollbackSettings {
	namespaces := make([]string, 0, len(app.Namespaces))
	for _, binding := range app.Namespaces {
		namespaces = append(namespaces, binding.Namespace)
	}
	sort.Strings(namespaces)
	return core.RollbackSettings{SourceID: app.SourceID, Revision: app.Revision, ManifestPath: app.ManifestPath, TargetManifestPath: app.TargetManifestPath, NamespaceManifestPaths: app.NamespaceManifestPaths, Renderer: app.Renderer, KustomizeHelmEnabled: app.KustomizeHelmEnabled, KustomizeNamespaceOverride: app.KustomizeNamespaceOverride, HelmReleaseName: app.HelmReleaseName, HelmValuesFiles: app.HelmValuesFiles, HelmValuesYAML: app.HelmValuesYAML, TargetHelmValuesFiles: app.TargetHelmValuesFiles, TargetHelmValuesYAML: app.TargetHelmValuesYAML, NamespaceHelmValues: app.NamespaceHelmValues, ClusterID: app.ClusterID, Namespaces: namespaces}
}

func (s *Service) saveRollbackSnapshot(ctx context.Context, app store.Application, operationID, kind, revision string, settings core.RollbackSettings, resources []core.Resource) (store.RollbackSnapshot, error) {
	id := store.NewID()
	payload, err := json.Marshal(rollbackSnapshotPayload{Version: 1, Settings: settings, ResolvedRevision: revision, OperationID: operationID, Resources: resources})
	if err != nil {
		return store.RollbackSnapshot{}, err
	}
	ciphertext, err := security.Encrypt(s.EncryptionKey, payload, "rollback-snapshot:"+app.ID+":"+id)
	if err != nil {
		return store.RollbackSnapshot{}, err
	}
	snapshot := store.RollbackSnapshot{ID: id, ApplicationID: app.ID, OperationID: operationID, Kind: kind, Revision: revision, ResourceCount: len(resources), PayloadCipher: ciphertext}
	if err := s.Store.SaveRollbackSnapshot(ctx, snapshot); err != nil {
		return store.RollbackSnapshot{}, err
	}
	return snapshot, nil
}

// captureManagedLiveState returns only fields recorded as managed in JustCD's
// last-applied manifests, workspaceed from the live object at checkpoint time.
// It deliberately does not apply current ignore rules here: a rollback plan
// must be able to show when a present-day rule prevents restoring part of the
// complete captured checkpoint.
func (s *Service) captureManagedLiveState(ctx context.Context, app store.Application, input planInput) ([]core.Resource, error) {
	live, _, err := s.liveSnapshot(ctx, input, nil)
	if err != nil {
		return nil, err
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	previous := make(map[string][]byte, len(managed))
	for _, item := range managed {
		item.Identity.ClusterScoped = item.Identity.Namespace == ""
		previous[item.Identity.Key()] = item.Manifest
	}
	state := make([]core.Resource, 0, len(live))
	for _, item := range live {
		lastApplied := previous[item.Identity.Key()]
		if len(lastApplied) == 0 {
			continue
		}
		before, _, err := render.ComparisonManifests(item.Manifest, lastApplied, lastApplied)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(before)
		state = append(state, core.Resource{Identity: item.Identity, Fingerprint: hex.EncodeToString(sum[:]), Manifest: before})
	}
	sort.Slice(state, func(i, j int) bool { return state[i].Identity.Key() < state[j].Identity.Key() })
	return state, nil
}

func (s *Service) SavePreOperationCheckpoint(ctx context.Context, app store.Application, operationID string, input planInput) (store.RollbackSnapshot, error) {
	resources, err := s.captureManagedLiveState(ctx, app, input)
	if err != nil {
		return store.RollbackSnapshot{}, err
	}
	return s.saveRollbackSnapshot(ctx, app, operationID, "pre_operation", app.LastSyncedRevision, rollbackSettings(app), resources)
}

func (s *Service) SaveSuccessfulDeploymentSnapshot(ctx context.Context, app store.Application, operationID string, revision string, settings core.RollbackSettings, input planInput) (store.RollbackSnapshot, error) {
	resources, err := s.captureManagedLiveState(ctx, app, input)
	if err != nil {
		return store.RollbackSnapshot{}, err
	}
	if revision != "" {
		settings.Revision = revision
	}
	return s.saveRollbackSnapshot(ctx, app, operationID, "successful_sync", revision, settings, resources)
}

func (s *Service) RollbackTargets(ctx context.Context, applicationID string) ([]store.RollbackTargetSummary, error) {
	return s.Store.ListRollbackTargets(ctx, applicationID)
}

func (s *Service) BuildRollbackPlan(ctx context.Context, app store.Application, actorID, targetKind, targetID, revision string) (store.PlanRecord, error) {
	targetKind = strings.TrimSpace(targetKind)
	if targetKind == "git_revision" {
		if strings.TrimSpace(revision) == "" {
			return store.PlanRecord{}, errors.New("a Git revision is required")
		}
		target := core.RollbackTarget{Kind: targetKind, Revision: strings.TrimSpace(revision), Settings: rollbackSettings(app)}
		return s.buildRollbackRecord(ctx, app, actorID, target)
	}
	if targetKind != "successful_sync" && targetKind != "pre_operation" {
		return store.PlanRecord{}, errors.New("unsupported rollback target")
	}
	targets, err := s.Store.ListRollbackTargets(ctx, app.ID)
	if err != nil {
		return store.PlanRecord{}, err
	}
	var summary *store.RollbackTargetSummary
	for i := range targets {
		if targets[i].ID == targetID && targets[i].Kind == targetKind {
			summary = &targets[i]
			break
		}
	}
	if summary == nil {
		return store.PlanRecord{}, errors.New("rollback target is no longer available")
	}
	target := core.RollbackTarget{Kind: summary.Kind, ID: summary.ID, OperationID: summary.OperationID, Revision: summary.Revision, CreatedAt: summary.CreatedAt, ResourceCount: summary.ResourceCount}
	return s.buildRollbackRecord(ctx, app, actorID, target)
}

func (s *Service) buildRollbackRecord(ctx context.Context, app store.Application, actorID string, target core.RollbackTarget) (store.PlanRecord, error) {
	plan, desired, err := s.CalculateRollbackPlan(ctx, app, target)
	if err != nil {
		return store.PlanRecord{}, err
	}
	now := time.Now().UTC()
	record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: desired, CreatedBy: actorID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Status: "current"}
	if err := s.Store.SavePlan(ctx, record); err != nil {
		return store.PlanRecord{}, err
	}
	_ = s.Store.Audit(ctx, actorID, "rollback.plan_created", "application", app.ID, map[string]any{"planId": record.ID, "targetKind": target.Kind, "targetId": target.ID, "targetRevision": plan.Rollback.Revision, "digest": plan.Digest, "changes": len(plan.Changes)})
	return record, nil
}

func (s *Service) CalculateRollbackPlan(ctx context.Context, app store.Application, target core.RollbackTarget) (core.Plan, []core.Resource, error) {
	if app.Decommissioning {
		return core.Plan{}, nil, errors.New("application is being decommissioned; cancel deletion before rollback")
	}
	if target.Kind == "git_revision" {
		if target.Revision == "" {
			return core.Plan{}, nil, errors.New("rollback Git revision is missing")
		}
		gitApp := app
		gitApp.Revision = target.Revision
		plan, desired, err := s.CalculatePlanWithSelection(ctx, gitApp, core.PlanSelection{})
		if err != nil {
			return core.Plan{}, nil, err
		}
		target.Revision = plan.Revision
		target.Settings = rollbackSettings(app)
		plan.Rollback = &target
		if err := s.requireRollbackApprovals(ctx, app, &plan); err != nil {
			return core.Plan{}, nil, err
		}
		if err := s.validateRollbackDryRun(ctx, app, plan, desired); err != nil {
			return core.Plan{}, nil, err
		}
		if err := core.RefreshDigest(&plan); err != nil {
			return core.Plan{}, nil, err
		}
		return plan, desired, nil
	}
	if target.Kind != "successful_sync" && target.Kind != "pre_operation" || target.ID == "" {
		return core.Plan{}, nil, errors.New("rollback snapshot target is invalid")
	}
	snapshot, err := s.Store.RollbackSnapshotByID(ctx, app.ID, target.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Plan{}, nil, errors.New("rollback target is no longer available")
		}
		return core.Plan{}, nil, err
	}
	if snapshot.Kind != target.Kind || (target.OperationID != "" && snapshot.OperationID != target.OperationID) {
		return core.Plan{}, nil, errors.New("rollback target provenance changed")
	}
	plaintext, err := security.Decrypt(s.EncryptionKey, snapshot.PayloadCipher, "rollback-snapshot:"+app.ID+":"+snapshot.ID)
	if err != nil {
		return core.Plan{}, nil, errors.New("rollback snapshot cannot be decrypted")
	}
	var payload rollbackSnapshotPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil || payload.Version != 1 {
		return core.Plan{}, nil, errors.New("rollback snapshot is invalid")
	}
	if payload.OperationID != snapshot.OperationID || payload.ResolvedRevision != snapshot.Revision || len(payload.Resources) != snapshot.ResourceCount {
		return core.Plan{}, nil, errors.New("rollback snapshot provenance or resource count is inconsistent")
	}
	if payload.Settings.ClusterID != app.ClusterID || !sameNamespaces(payload.Settings.Namespaces, namespaceNames(app.Namespaces)) {
		return core.Plan{}, nil, errors.New("rollback target scope no longer matches this application's cluster and namespaces")
	}
	target.Settings = payload.Settings
	target.Revision = payload.ResolvedRevision
	target.CreatedAt = snapshot.CreatedAt
	target.ResourceCount = len(payload.Resources)
	plan, desired, err := s.calculateSnapshotPlan(ctx, app, target, payload.Resources)
	if err != nil {
		return core.Plan{}, nil, err
	}
	return plan, desired, nil
}

func (s *Service) calculateSnapshotPlan(ctx context.Context, app store.Application, target core.RollbackTarget, resources []core.Resource) (core.Plan, []core.Resource, error) {
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Plan{}, nil, err
	}
	input.IgnoreRules, err = s.Store.IgnoreRules(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	input.IgnoreSelectors, err = s.Store.IgnoreSelectors(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	allowed := make(map[string]bool, len(input.Bindings))
	for _, binding := range input.Bindings {
		key := binding.ClusterID + "\x00" + binding.Namespace
		if binding.ClusterScope {
			key = binding.ClusterID + "\x00*"
		}
		allowed[key] = true
	}
	desiredForCompare := make([]core.Resource, 0, len(resources))
	ignoredResourceChanges := make([]core.Change, 0)
	for _, resource := range resources {
		identity := resource.Identity
		if identity.ClusterID == "" {
			identity.ClusterID = app.ClusterID
		}
		identity.ClusterScoped = identity.Namespace == ""
		key := identity.ClusterID + "\x00" + identity.Namespace
		if identity.ClusterScoped {
			key = identity.ClusterID + "\x00*"
		}
		if !allowed[key] {
			return core.Plan{}, nil, fmt.Errorf("rollback resource %s %s/%s is outside this application's bound scope", identity.Kind, identity.Namespace, identity.Name)
		}
		resource.Identity = identity
		ignoreReason := ""
		for _, rule := range input.IgnoreRules {
			if rule.Identity.Key() == identity.Key() && rule.Path == "" {
				ignoreReason = rule.Reason
				break
			}
		}
		if ignoreReason == "" && selectorMatchesManifest(input.IgnoreSelectors, identity, resource.Manifest) {
			ignoreReason = "Excluded by an application label selector"
		}
		if ignoreReason != "" {
			ignoredResourceChanges = append(ignoredResourceChanges, core.Change{Kind: core.Update, Identity: identity, IgnoreReason: ignoreReason})
			continue
		}
		// Keep field ignores present while the live state is fingerprinted so
		// the planner can validate the handoff and exclude them from drift.
		desiredForCompare = append(desiredForCompare, resource)
	}
	input.Selection = core.PlanSelection{}
	live, changedIgnoredFields, err := s.liveSnapshot(ctx, input, desiredForCompare)
	if err != nil {
		return core.Plan{}, nil, err
	}
	planRevision := target.Revision
	if planRevision == "" {
		planRevision = "pre-operation:" + target.OperationID
	}
	plan, err := core.BuildPlan(app.ID, planRevision, input.Bindings, desiredForCompare, live)
	if err != nil {
		return core.Plan{}, nil, err
	}
	desired := append([]core.Resource(nil), desiredForCompare...)
	for index := range plan.Changes {
		if plan.Changes[index].Kind == core.Update {
			plan.Changes[index].Before, plan.Changes[index].After, err = render.ComparisonManifests(plan.Changes[index].Before, plan.Changes[index].After, managedManifest(ctx, s.Store, app.ID, plan.Changes[index].Identity))
			if err != nil {
				return core.Plan{}, nil, err
			}
		}
		for _, path := range persistentPathsFor(input, plan.Changes[index].Identity) {
			if len(plan.Changes[index].Before) > 0 {
				plan.Changes[index].Before, err = render.RemoveJSONPointer(plan.Changes[index].Before, path)
				if err != nil {
					return core.Plan{}, nil, err
				}
			}
			if len(plan.Changes[index].After) > 0 {
				plan.Changes[index].After, err = render.RemoveJSONPointer(plan.Changes[index].After, path)
				if err != nil {
					return core.Plan{}, nil, err
				}
			}
		}
		plan.Changes[index].ChangedPaths, err = render.ChangedJSONPointers(plan.Changes[index].Before, plan.Changes[index].After)
		if err != nil {
			return core.Plan{}, nil, err
		}
	}
	for index := range desired {
		for _, path := range persistentPathsFor(input, desired[index].Identity) {
			desired[index].Manifest, err = render.RemoveJSONPointer(desired[index].Manifest, path)
			if err != nil {
				return core.Plan{}, nil, err
			}
		}
	}
	plan.Ignored = append(plan.Ignored, ignoredResourceChanges...)
	ignoredKeys := make([]string, 0, len(changedIgnoredFields))
	for key := range changedIgnoredFields {
		ignoredKeys = append(ignoredKeys, key)
	}
	sort.Strings(ignoredKeys)
	for _, key := range ignoredKeys {
		identity := identityForKey(desiredForCompare, key)
		plan.Ignored = append(plan.Ignored, core.Change{Kind: core.Update, Identity: identity, ChangedPaths: changedIgnoredFields[key], IgnoredPaths: changedIgnoredFields[key], IgnoreReason: "Excluded by current persistent field rules"})
	}
	plan.Rollback = &target
	if err := s.requireRollbackApprovals(ctx, app, &plan); err != nil {
		return core.Plan{}, nil, err
	}
	if err := s.validateRollbackDryRun(ctx, app, plan, desired); err != nil {
		return core.Plan{}, nil, err
	}
	if err := core.RefreshDigest(&plan); err != nil {
		return core.Plan{}, nil, err
	}
	return plan, desired, nil
}

func managedManifest(ctx context.Context, s *store.Store, appID string, identity core.Identity) []byte {
	resources, err := s.ManagedResources(ctx, appID)
	if err != nil {
		return nil
	}
	for _, resource := range resources {
		resource.Identity.ClusterScoped = resource.Identity.Namespace == ""
		if resource.Identity.Key() == identity.Key() {
			return resource.Manifest
		}
	}
	return nil
}

func (s *Service) requireRollbackApprovals(ctx context.Context, app store.Application, plan *core.Plan) error {
	policy, err := s.Store.EffectiveApprovalPolicy(ctx, app)
	if err != nil {
		return err
	}
	required := 1
	for _, change := range plan.Changes {
		if change.Kind == core.Delete && policy.Deletion.RequiredApprovals > required {
			required = policy.Deletion.RequiredApprovals
		}
	}
	plan.ApprovalKind = "rollback"
	plan.RequiredApprovals = required
	plan.ApproverRoles = []string{"owner"}
	plan.ApproverUserIDs = []string{}
	plan.RequiresApproval = true
	return nil
}

func (s *Service) validateRollbackDryRun(ctx context.Context, app store.Application, plan core.Plan, desired []core.Resource) error {
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return err
	}
	desiredByKey := make(map[string]core.Resource, len(desired))
	for _, resource := range desired {
		desiredByKey[resource.Identity.Key()] = resource
	}
	for _, change := range plan.Changes {
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(change.Identity), groupVersion(change.Identity))
		if err != nil {
			return fmt.Errorf("rollback dry-run cannot discover %s: %w", change.Identity.Kind, err)
		}
		client, err := clientFor(input, change.Identity)
		if err != nil {
			return err
		}
		var resourceClient dynamic.ResourceInterface
		if mapping.Scope.Name() == "namespace" {
			resourceClient = client.Dynamic.Resource(mapping.Resource).Namespace(change.Identity.Namespace)
		} else {
			resourceClient = client.Dynamic.Resource(mapping.Resource)
		}
		switch change.Kind {
		case core.Create, core.Update:
			resource, exists := desiredByKey[change.Identity.Key()]
			if !exists {
				return errors.New("rollback plan desired resource is missing")
			}
			object, err := render.NormalizedObject(resource)
			if err != nil {
				return err
			}
			object.SetLabels(mergeOwnerLabel(object.GetLabels(), app.ID))
			if change.Kind == core.Create {
				_, err = resourceClient.Create(ctx, object, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: "justcd/" + app.ID})
			} else {
				object.SetResourceVersion(change.LiveResourceVersion)
				body, marshalErr := object.MarshalJSON()
				if marshalErr != nil {
					return marshalErr
				}
				options := applyPatchOptions(change, "justcd/"+app.ID)
				options.DryRun = []string{metav1.DryRunAll}
				_, err = resourceClient.Patch(ctx, change.Identity.Name, types.ApplyPatchType, body, options)
			}
		case core.Delete:
			uid, rv := types.UID(change.LiveUID), change.LiveResourceVersion
			err = resourceClient.Delete(ctx, change.Identity.Name, metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
		default:
			return errors.New("rollback plan contains an unsupported change")
		}
		if apierrors.IsMethodNotSupported(err) {
			// Some older aggregated APIs do not implement dry-run. The normal
			// UID/ownership/resourceVersion checks still run at approval and apply.
			continue
		}
		if err != nil {
			return fmt.Errorf("Kubernetes rejected rollback dry-run for %s %s/%s: %w", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, err)
		}
	}
	return nil
}

func namespaceNames(bindings []store.NamespaceBinding) []string {
	names := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		names = append(names, binding.Namespace)
	}
	sort.Strings(names)
	return names
}

func sameNamespaces(left, right []string) bool {
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	return len(a) == len(b) && strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func (s *Service) ValidateResumeRevision(ctx context.Context, app store.Application, revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", errors.New("a Git revision is required")
	}
	if app.RollbackResumeState != nil {
		state := app.RollbackResumeState
		app.SourceID, app.ManifestPath, app.Renderer = state.SourceID, state.ManifestPath, state.Renderer
		app.TargetManifestPath, app.NamespaceManifestPaths = state.TargetManifestPath, state.NamespaceManifestPaths
		app.KustomizeHelmEnabled, app.KustomizeNamespaceOverride = state.KustomizeHelmEnabled, state.KustomizeNamespaceOverride
		app.HelmValuesFiles, app.HelmValuesYAML = state.HelmValuesFiles, state.HelmValuesYAML
		app.TargetHelmValuesFiles, app.TargetHelmValuesYAML = state.TargetHelmValuesFiles, state.TargetHelmValuesYAML
		app.NamespaceHelmValues = state.NamespaceHelmValues
		app.ClusterID, app.Namespaces = state.ClusterID, state.Namespaces
	}
	app.Revision = revision
	plan, _, err := s.CalculatePlanWithSelection(ctx, app, core.PlanSelection{})
	if err != nil {
		return "", err
	}
	return plan.Revision, nil
}

// ValidateRollbackResourceStep revalidates the immutable review, target,
// owner approvals, and current Kubernetes credentials immediately before a
// rollback mutates each resource. The target Git commit was fetched and
// rendered at approval, queueing, and worker preflight; it is stored as an
// immutable commit SHA in the plan, so a moving branch cannot retarget a step.
func (s *Service) ValidateRollbackResourceStep(ctx context.Context, operation store.Operation, expected store.PlanRecord, expectedApp store.Application, actorID string) (planInput, error) {
	if expected.Plan.Rollback == nil {
		return planInput{}, errors.New("rollback plan target is missing")
	}
	stored, err := s.Store.PlanByID(ctx, expected.ID)
	if err != nil {
		return planInput{}, errors.New("rollback plan is no longer available")
	}
	recomputed := stored.Plan
	if err := core.RefreshDigest(&recomputed); err != nil {
		return planInput{}, err
	}
	if stored.Status != "current" || stored.Plan.Digest != expected.Plan.Digest || recomputed.Digest != expected.Plan.Digest {
		return planInput{}, errors.New("rollback plan digest changed during operation")
	}
	if stored.Plan.Rollback == nil || !reflect.DeepEqual(stored.Plan.Rollback, expected.Plan.Rollback) {
		return planInput{}, errors.New("rollback target changed during operation")
	}
	app, err := s.Store.ApplicationByID(ctx, expectedApp.ID)
	if err != nil || !sameRollbackApplicationConfig(app, expectedApp) {
		return planInput{}, errors.New("application settings changed during rollback")
	}
	if err := s.validateRollbackTargetStillAvailable(ctx, app, *expected.Plan.Rollback); err != nil {
		return planInput{}, err
	}
	if err := s.validateRollbackOwnerApprovals(ctx, app, operation, expected, actorID); err != nil {
		return planInput{}, err
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return planInput{}, fmt.Errorf("recheck Kubernetes credentials before rollback step: %w", err)
	}
	if !samePlanBindings(input.Bindings, expected.Plan.Bindings) {
		return planInput{}, errors.New("Kubernetes credentials or scope changed during rollback")
	}
	return input, nil
}

func sameRollbackApplicationConfig(left, right store.Application) bool {
	return left.ID == right.ID && left.WorkspaceID == right.WorkspaceID && left.SourceID == right.SourceID && left.Revision == right.Revision && left.ManifestPath == right.ManifestPath && left.TargetManifestPath == right.TargetManifestPath && reflect.DeepEqual(left.NamespaceManifestPaths, right.NamespaceManifestPaths) && left.Renderer == right.Renderer && left.KustomizeHelmEnabled == right.KustomizeHelmEnabled && left.KustomizeNamespaceOverride == right.KustomizeNamespaceOverride && reflect.DeepEqual(left.HelmValuesFiles, right.HelmValuesFiles) && left.HelmValuesYAML == right.HelmValuesYAML && reflect.DeepEqual(left.TargetHelmValuesFiles, right.TargetHelmValuesFiles) && left.TargetHelmValuesYAML == right.TargetHelmValuesYAML && reflect.DeepEqual(left.NamespaceHelmValues, right.NamespaceHelmValues) && left.ClusterID == right.ClusterID && left.CreateNamespaces == right.CreateNamespaces && reflect.DeepEqual(left.Namespaces, right.Namespaces) && left.Decommissioning == right.Decommissioning
}

func samePlanBindings(left, right []core.Binding) bool {
	if len(left) != len(right) {
		return false
	}
	key := func(binding core.Binding) string {
		return binding.ClusterID + "\x00" + binding.Namespace + "\x00" + binding.CredentialRef + "\x00" + fmt.Sprint(binding.ClusterScope)
	}
	a, b := make([]string, 0, len(left)), make([]string, 0, len(right))
	for _, binding := range left {
		a = append(a, key(binding))
	}
	for _, binding := range right {
		b = append(b, key(binding))
	}
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func (s *Service) validateRollbackTargetStillAvailable(ctx context.Context, app store.Application, target core.RollbackTarget) error {
	switch target.Kind {
	case "successful_sync", "pre_operation":
		snapshot, err := s.Store.RollbackSnapshotByID(ctx, app.ID, target.ID)
		if err != nil || snapshot.Kind != target.Kind || snapshot.Revision != target.Revision || (target.OperationID != "" && snapshot.OperationID != target.OperationID) {
			return errors.New("rollback checkpoint changed or is no longer available")
		}
		plain, err := security.Decrypt(s.EncryptionKey, snapshot.PayloadCipher, "rollback-snapshot:"+app.ID+":"+snapshot.ID)
		if err != nil {
			return errors.New("rollback checkpoint integrity check failed")
		}
		var payload rollbackSnapshotPayload
		if err := json.Unmarshal(plain, &payload); err != nil || payload.Version != 1 || len(payload.Resources) != snapshot.ResourceCount || payload.ResolvedRevision != target.Revision || !reflect.DeepEqual(payload.Settings, target.Settings) {
			return errors.New("rollback checkpoint contents changed")
		}
		return nil
	case "git_revision":
		if target.Revision == "" {
			return errors.New("rollback Git target is missing its resolved commit")
		}
		source, err := s.Store.GitSourceForWorkspace(ctx, target.Settings.SourceID, app.WorkspaceID)
		if err != nil {
			return errors.New("rollback Git source is no longer available to this workspace")
		}
		if source.CredentialID == nil {
			return nil
		}
		credential, err := s.Store.CredentialByID(ctx, *source.CredentialID)
		if err != nil || (credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) {
			return errors.New("rollback Git credential is unavailable or expired")
		}
		plain, err := security.Decrypt(s.EncryptionKey, credential.Cipher, "credential:"+credential.ID)
		if err != nil {
			return errors.New("rollback Git credential cannot be decrypted")
		}
		var secrets gitops.Credentials
		if err := json.Unmarshal(plain, &secrets); err != nil {
			return errors.New("rollback Git credential payload is invalid")
		}
		switch credential.Kind {
		case "git-https":
			if secrets.Token == "" {
				return errors.New("rollback Git credential has no token")
			}
		case "git-ssh":
			if secrets.PrivateKey == "" || secrets.KnownHosts == "" {
				return errors.New("rollback Git credential is incomplete")
			}
		default:
			return errors.New("rollback Git credential kind is unsupported")
		}
		return nil
	default:
		return errors.New("rollback target kind is unsupported")
	}
}

func (s *Service) validateRollbackOwnerApprovals(ctx context.Context, app store.Application, operation store.Operation, record store.PlanRecord, actorID string) error {
	actor, err := s.Store.UserByID(ctx, actorID)
	if err != nil {
		return errors.New("rollback actor is no longer available")
	}
	role, err := s.Store.WorkspaceRole(ctx, actor, app.WorkspaceID)
	if err != nil || role != "owner" {
		return errors.New("rollback actor is no longer a workspace owner")
	}
	approvalIDs := operation.ApprovalIDs
	if len(approvalIDs) == 0 && operation.ApprovalID != "" {
		approvalIDs = []string{operation.ApprovalID}
	}
	if len(approvalIDs) < core.RequiredApprovalCount(record.Plan) {
		return errors.New("rollback approval is no longer available")
	}
	seen := make(map[string]bool, len(approvalIDs))
	approvals := make([]core.DeletionApproval, 0, len(approvalIDs))
	for _, approvalID := range approvalIDs {
		stored, err := s.Store.ApprovalByID(ctx, approvalID)
		if err != nil || stored.PlanID != record.ID || stored.UsedAt == nil || stored.Approval.PlanDigest != record.Plan.Digest || seen[stored.Approval.ActorID] {
			return errors.New("rollback approval changed during operation")
		}
		seen[stored.Approval.ActorID] = true
		approver, err := s.Store.UserByID(ctx, stored.Approval.ActorID)
		if err != nil {
			return errors.New("rollback approver is no longer available")
		}
		role, err := s.Store.WorkspaceRole(ctx, approver, app.WorkspaceID)
		if err != nil || role != "owner" || !core.ApprovalRoleAllows(record.Plan, role, stored.Approval.ActorID) {
			return errors.New("rollback approver is no longer a workspace owner")
		}
		approvals = append(approvals, stored.Approval)
	}
	// Approval expiry is evaluated at durable queue time: a long-running
	// operation must not lose its authorization merely because execution takes
	// longer than the approval's pre-queue expiry window.
	if err := core.AuthorizeApplyMany(record.Plan, approvals, operation.StartedAt); err != nil {
		return fmt.Errorf("rollback approval no longer matches the reviewed change set: %w", err)
	}
	return nil
}
