package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Service struct {
	Store         *store.Store
	EncryptionKey []byte
	Metrics       *observability.Metrics
}

type planStageError struct {
	stage string
	err   error
}

func (e *planStageError) Error() string      { return e.err.Error() }
func (e *planStageError) Unwrap() error      { return e.err }
func (e *planStageError) ErrorStage() string { return e.stage }

func statusIssue(stage string, at time.Time) store.ApplicationStatusIssue {
	summary := "Application check failed. Review the plan error for details."
	switch stage {
	case "git":
		summary = "Git source could not be reached or read. Check the repository URL, credentials, and revision."
	case "cluster":
		summary = "Kubernetes API could not be reached or read. Check cluster connectivity and credentials."
	case "render":
		summary = "Manifests could not be rendered. Check the source configuration."
	}
	return store.ApplicationStatusIssue{Source: stage, Summary: summary, ObservedAt: at}
}

// OwnershipConflict is returned when a rendered object already exists but has
// never been claimed by this application. It contains only review metadata;
// callers must recheck the object before adopting it.
type OwnershipConflict struct {
	Identity           core.Identity `json:"identity"`
	UID                string        `json:"uid"`
	ResourceVersion    string        `json:"resourceVersion"`
	Owner              string        `json:"owner,omitempty"`
	OwnerMissing       bool          `json:"ownerMissing,omitempty"`
	DesiredFingerprint string        `json:"desiredFingerprint"`
	HasOwnerReferences bool          `json:"hasOwnerReferences"`
	DesiredManifest    []byte        `json:"-"`
}

func (e *OwnershipConflict) Error() string {
	return fmt.Sprintf("%s %s/%s exists but is not recorded as managed by this application", e.Identity.Kind, e.Identity.Namespace, e.Identity.Name)
}

type OwnershipConflicts struct {
	Items []*OwnershipConflict
}

func (e *OwnershipConflicts) Error() string {
	return fmt.Sprintf("%d rendered resources already exist but are not managed by this application", len(e.Items))
}

// Keep callers that inspect a single conflict compatible with the first item.
func (e *OwnershipConflicts) Unwrap() error {
	if len(e.Items) == 0 {
		return nil
	}
	return e.Items[0]
}

type planInput struct {
	Application        store.Application
	Cluster            store.Cluster
	Bindings           []core.Binding
	NamespaceBindings  map[string]store.NamespaceBinding
	NamespaceClients   map[string]*kube.Clients
	ClusterScopeClient *kube.Clients
	Mapper             *kube.Clients
	IgnoreRules        []store.ApplicationIgnoreRule
	IgnoreSelectors    []core.IgnoreSelector
	Selection          core.PlanSelection
}

func (s *Service) BuildPlan(ctx context.Context, applicationID, actorID string) (store.PlanRecord, error) {
	app, err := s.Store.ApplicationByID(ctx, applicationID)
	if err != nil {
		return store.PlanRecord{}, err
	}
	return s.BuildPlanWithSelection(ctx, app, actorID, core.PlanSelection{})
}

func (s *Service) BuildPlanWithSelection(ctx context.Context, app store.Application, actorID string, selection core.PlanSelection) (store.PlanRecord, error) {
	ctx, span := otel.Tracer("justcd/planner").Start(ctx, "plan.build")
	defer span.End()
	span.SetAttributes(attribute.String("application.id", app.ID))
	plan, desired, err := s.CalculatePlanWithSelection(ctx, app, selection)
	if err != nil {
		observability.MarkError(span, err)
		if ctx.Err() == nil {
			var stageErr *planStageError
			issues := []store.ApplicationStatusIssue{statusIssue("check", time.Now().UTC())}
			if errors.As(err, &stageErr) {
				issues[0] = statusIssue(stageErr.stage, time.Now().UTC())
				if stageErr.stage == "git" {
					// Git failure prevents the normal plan from probing Kubernetes.
					// Check it independently so simultaneous outages remain visible.
					if _, clusterErr := s.loadPlanInput(ctx, app); clusterErr != nil && ctx.Err() == nil {
						issues = append(issues, statusIssue("cluster", time.Now().UTC()))
					}
				}
			}
			_ = s.Store.SetApplicationStatusIssues(ctx, app.ID, issues)
		}
		return store.PlanRecord{}, err
	}
	now := time.Now().UTC()
	record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: desired, CreatedBy: actorID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Status: "current"}
	span.SetAttributes(attribute.String("plan.id", record.ID), attribute.Int("plan.change_count", len(plan.Changes)), attribute.Bool("plan.requires_approval", plan.RequiresApproval))
	if err := s.Store.SavePlan(ctx, record); err != nil {
		observability.MarkError(span, err)
		return store.PlanRecord{}, err
	}
	if _, err := s.Store.DB.ExecContext(ctx, `UPDATE applications SET health=$2,status_issues='[]'::jsonb,last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, app.ID, healthForPlan(plan)); err != nil {
		observability.MarkError(span, err)
		return store.PlanRecord{}, err
	}
	_ = s.Store.Audit(ctx, actorID, "plan.created", "application", app.ID, map[string]any{"planId": record.ID, "revision": plan.Revision, "digest": plan.Digest, "changes": len(plan.Changes)})
	return record, nil
}

func (s *Service) BuildDecommissionPlan(ctx context.Context, app store.Application, actorID string) (store.PlanRecord, error) {
	if app.Decommissioning {
		current, err := s.Store.ListPlans(ctx, app.ID, 1)
		if err == nil && len(current) > 0 && current[0].Plan.Decommission && current[0].Status == "current" && time.Now().Before(current[0].ExpiresAt) {
			return current[0], nil
		}
	}
	plan, err := s.CalculateDecommissionPlan(ctx, app)
	if err != nil {
		return store.PlanRecord{}, err
	}
	now := time.Now().UTC()
	record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: []core.Resource{}, CreatedBy: actorID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), Status: "current"}
	if err := s.Store.SavePlan(ctx, record); err != nil {
		return store.PlanRecord{}, err
	}
	_ = s.Store.Audit(ctx, actorID, "application.decommission_plan_created", "application", app.ID, map[string]any{"planId": record.ID, "deletions": len(plan.Changes), "digest": plan.Digest})
	return record, nil
}

func (s *Service) CalculateDecommissionPlan(ctx context.Context, app store.Application) (core.Plan, error) {
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Plan{}, err
	}
	live, _, err := s.liveSnapshot(ctx, input, nil)
	if err != nil {
		return core.Plan{}, err
	}
	revision := app.LastSyncedRevision
	if revision == "" {
		revision = "decommission"
	}
	plan, err := core.BuildPlan(app.ID, revision, input.Bindings, nil, live)
	if err != nil {
		return core.Plan{}, err
	}
	plan.Decommission = true
	policy, err := s.Store.EffectiveApprovalPolicy(ctx, app)
	if err != nil {
		return core.Plan{}, err
	}
	setApprovalRule(&plan, "deletion", policy.Deletion)
	if err := core.RefreshDigest(&plan); err != nil {
		return core.Plan{}, err
	}
	return plan, nil
}

func (s *Service) RecheckPlan(ctx context.Context, app store.Application, record store.PlanRecord) (core.Plan, error) {
	if record.Plan.Rollback != nil {
		plan, _, err := s.CalculateRollbackPlan(ctx, app, *record.Plan.Rollback)
		return plan, err
	}
	if record.Plan.Decommission {
		if !app.Decommissioning {
			return core.Plan{}, errors.New("decommission plan was cancelled")
		}
		return s.CalculateDecommissionPlan(ctx, app)
	}
	if app.Decommissioning {
		return core.Plan{}, errors.New("application is being decommissioned; cancel deletion before normal sync")
	}
	plan, _, err := s.CalculatePlanWithSelection(ctx, app, record.Plan.Selection)
	return plan, err
}

func (s *Service) RefreshPlan(ctx context.Context, app store.Application, actorID string, record store.PlanRecord) (store.PlanRecord, error) {
	if record.Plan.Rollback != nil {
		return s.buildRollbackRecord(ctx, app, actorID, *record.Plan.Rollback)
	}
	if record.Plan.Decommission {
		if !app.Decommissioning {
			return store.PlanRecord{}, errors.New("decommission plan was cancelled")
		}
		return s.BuildDecommissionPlan(ctx, app, actorID)
	}
	return s.BuildPlanWithSelection(ctx, app, actorID, record.Plan.Selection)
}

func (s *Service) CalculatePlan(ctx context.Context, app store.Application) (core.Plan, []core.Resource, error) {
	return s.CalculatePlanWithSelection(ctx, app, core.PlanSelection{})
}

func (s *Service) CalculatePlanWithSelection(ctx context.Context, app store.Application, selection core.PlanSelection) (core.Plan, []core.Resource, error) {
	ctx, span := otel.Tracer("justcd/planner").Start(ctx, "plan.calculate")
	defer span.End()
	span.SetAttributes(attribute.String("application.id", app.ID), attribute.String("renderer", app.Renderer))
	if app.ConfigurationMissing {
		return core.Plan{}, nil, errors.New("application definition is missing from Git; restore it before syncing")
	}
	if app.Decommissioning {
		return core.Plan{}, nil, errors.New("application is being decommissioned; cancel deletion before normal sync")
	}
	rules, err := s.Store.IgnoreRules(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	selectors, err := s.Store.IgnoreSelectors(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	selection = normalizeSelection(selection)
	source, err := s.Store.GitSourceForWorkspace(ctx, app.SourceID, app.WorkspaceID)
	if err != nil {
		return core.Plan{}, nil, &planStageError{stage: "git", err: errors.New("application Git source is unavailable")}
	}
	checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, app.Revision)
	if err != nil {
		return core.Plan{}, nil, &planStageError{stage: "git", err: err}
	}
	defer checkout.Close()
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Plan{}, nil, &planStageError{stage: "cluster", err: err}
	}
	input.IgnoreRules = rules
	input.IgnoreSelectors = selectors
	input.Selection = selection
	namespaceBindings := make([]core.Binding, 0, len(input.Bindings))
	for _, binding := range input.Bindings {
		if !binding.ClusterScope {
			namespaceBindings = append(namespaceBindings, binding)
		}
	}
	ignoredResources := make([]core.Identity, 0, len(rules))
	for _, rule := range rules {
		if rule.Path == "" {
			ignoredResources = append(ignoredResources, rule.Identity)
		}
	}
	desired, err := render.Render(ctx, render.Options{RepositoryRoot: checkout.Root, ManifestPath: app.ManifestPath, TargetManifestPath: app.TargetManifestPath, NamespaceManifestPaths: app.NamespaceManifestPaths, Renderer: app.Renderer, KustomizeHelmEnabled: app.KustomizeHelmEnabled, KustomizeNamespaceOverride: app.KustomizeNamespaceOverride, HelmReleaseName: app.HelmReleaseName, HelmValuesFiles: app.HelmValuesFiles, HelmValuesYAML: app.HelmValuesYAML, TargetHelmValuesFiles: app.TargetHelmValuesFiles, TargetHelmValuesYAML: app.TargetHelmValuesYAML, NamespaceHelmValues: app.NamespaceHelmValues, ApplicationID: app.ID, ClusterID: app.ClusterID, Namespaces: namespaceBindings, Mapper: input.Mapper.Mapper, IgnoredResources: ignoredResources, IgnoredSelectors: selectors})
	if err != nil {
		observability.MarkError(span, err)
		return core.Plan{}, nil, &planStageError{stage: "render", err: err}
	}
	for _, resource := range desired {
		if resource.Identity.ClusterScoped && input.ClusterScopeClient == nil {
			return core.Plan{}, nil, fmt.Errorf("cluster-scoped resource %s/%s requires a privileged cluster-scope credential", resource.Identity.Kind, resource.Identity.Name)
		}
	}
	live, changedIgnoredFields, err := s.liveSnapshot(ctx, input, desired)
	if err != nil {
		return core.Plan{}, nil, &planStageError{stage: "cluster", err: err}
	}
	plan, err := core.BuildPlan(app.ID, checkout.Commit, input.Bindings, desired, live)
	if err != nil {
		observability.MarkError(span, err)
		return core.Plan{}, nil, err
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	previousByKey := make(map[string][]byte, len(managed))
	for _, resource := range managed {
		previousByKey[resource.Identity.Key()] = resource.Manifest
	}
	for index := range plan.Changes {
		if plan.Changes[index].Kind == core.Update {
			plan.Changes[index].Before, plan.Changes[index].After, err = render.ComparisonManifests(plan.Changes[index].Before, plan.Changes[index].After, previousByKey[plan.Changes[index].Identity.Key()])
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
	plan.Selection = selection
	plan.IgnoreRulesDigest = ignoreRulesDigest(rules, selectors)
	plan.Ignored = append(plan.Ignored, applyResourceExclusions(&plan, rules, selection)...)
	ignoredKeys := make([]string, 0, len(changedIgnoredFields))
	for key := range changedIgnoredFields {
		ignoredKeys = append(ignoredKeys, key)
	}
	sort.Strings(ignoredKeys)
	for _, key := range ignoredKeys {
		paths := changedIgnoredFields[key]
		if len(paths) == 0 {
			continue
		}
		identity := identityForKey(desired, key)
		var changeIndex = -1
		for index := range plan.Changes {
			if plan.Changes[index].Identity.Key() == key {
				changeIndex = index
				break
			}
		}
		if changeIndex >= 0 {
			activeChange := &plan.Changes[changeIndex]
			ignoredBefore := append(json.RawMessage(nil), activeChange.Before...)
			ignoredAfter := append(json.RawMessage(nil), activeChange.After...)
			for _, path := range paths {
				if len(activeChange.Before) > 0 {
					activeChange.Before, err = render.RemoveJSONPointer(activeChange.Before, path)
					if err != nil {
						return core.Plan{}, nil, err
					}
				}
				if len(activeChange.After) > 0 {
					activeChange.After, err = render.RemoveJSONPointer(activeChange.After, path)
					if err != nil {
						return core.Plan{}, nil, err
					}
				}
			}
			activeChange.ChangedPaths, err = render.ChangedJSONPointers(activeChange.Before, activeChange.After)
			if err != nil {
				return core.Plan{}, nil, err
			}
			plan.Ignored = append(plan.Ignored, core.Change{Kind: core.Update, Identity: activeChange.Identity, Before: ignoredBefore, After: ignoredAfter, ChangedPaths: paths, IgnoredPaths: paths, IgnoreReason: "Fields are excluded from this plan's selection"})
			continue
		}
		var after json.RawMessage
		for _, resource := range desired {
			if resource.Identity.Key() == key {
				after = resource.Manifest
				identity = resource.Identity
				break
			}
		}
		var before json.RawMessage
		for _, resource := range live {
			if resource.Identity.Key() == key {
				before = resource.Manifest
				identity = resource.Identity
				break
			}
		}
		for _, path := range persistentPathsFor(input, identity) {
			if len(before) > 0 {
				before, err = render.RemoveJSONPointer(before, path)
				if err != nil {
					return core.Plan{}, nil, err
				}
			}
			if len(after) > 0 {
				after, err = render.RemoveJSONPointer(after, path)
				if err != nil {
					return core.Plan{}, nil, err
				}
			}
		}
		plan.Ignored = append(plan.Ignored, core.Change{Kind: core.Update, Identity: identity, Before: before, After: after, ChangedPaths: paths, IgnoredPaths: paths, IgnoreReason: "Fields are excluded by this plan's selection"})
	}
	for index := range plan.Changes {
		paths := ignoredPathsFor(input, plan.Changes[index].Identity)
		for _, path := range paths {
			for desiredIndex := range desired {
				if desired[desiredIndex].Identity.Key() == plan.Changes[index].Identity.Key() {
					clean, removeErr := render.RemoveJSONPointer(desired[desiredIndex].Manifest, path)
					if removeErr != nil {
						return core.Plan{}, nil, removeErr
					}
					desired[desiredIndex].Manifest = clean
				}
			}
		}
	}
	adopted := make(map[string]bool, len(managed))
	for _, resource := range managed {
		if resource.Adopted {
			adopted[resource.Identity.Key()] = true
		}
	}
	for index := range plan.Changes {
		if plan.Changes[index].Kind == core.Update && adopted[plan.Changes[index].Identity.Key()] {
			plan.Changes[index].Takeover = true
		}
	}
	if err := s.detectFieldTakeovers(ctx, input, &plan, desired); err != nil {
		return core.Plan{}, nil, err
	}
	if err := addNamespaceCreations(ctx, input, &plan, desired); err != nil {
		return core.Plan{}, nil, &planStageError{stage: "cluster", err: err}
	}
	policy, err := s.Store.EffectiveApprovalPolicy(ctx, app)
	if err != nil {
		return core.Plan{}, nil, err
	}
	kind := "sync"
	rule := policy.Sync
	hasDeletes, hasClusterScoped, hasTakeover := false, false, false
	for _, change := range plan.Changes {
		if change.Kind == core.Delete {
			hasDeletes = true
		}
		if change.Identity.ClusterScoped {
			hasClusterScoped = true
		}
		if change.Takeover {
			hasTakeover = true
		}
	}
	if hasDeletes {
		kind, rule = "deletion", policy.Deletion
	} else if hasClusterScoped && rule.RequiredApprovals == 0 {
		// Cluster-scoped changes keep the existing explicit owner approval
		// safeguard even when ordinary sync approvals are disabled.
		rule = store.ApprovalRule{RequiredApprovals: 1, ApproverRoles: []string{"owner"}, ApproverUserIDs: []string{}}
	}
	if hasTakeover {
		count := max(1, rule.RequiredApprovals)
		kind, rule = "takeover", store.ApprovalRule{RequiredApprovals: count, ApproverRoles: []string{"owner"}, ApproverUserIDs: []string{}}
	}
	setApprovalRule(&plan, kind, rule)
	if err := core.RefreshDigest(&plan); err != nil {
		return core.Plan{}, nil, err
	}
	return plan, desired, nil
}

func setApprovalRule(plan *core.Plan, kind string, rule store.ApprovalRule) {
	plan.ApprovalKind = kind
	plan.RequiredApprovals = 0
	plan.ApproverRoles = append([]string{}, rule.ApproverRoles...)
	plan.ApproverUserIDs = append([]string{}, rule.ApproverUserIDs...)
	if len(plan.Changes) > 0 {
		plan.RequiredApprovals = rule.RequiredApprovals
	}
	plan.RequiresApproval = plan.RequiredApprovals > 0
}

func normalizeSelection(selection core.PlanSelection) core.PlanSelection {
	sort.Slice(selection.Resources, func(i, j int) bool { return selection.Resources[i].Key() < selection.Resources[j].Key() })
	resources := selection.Resources[:0]
	for _, identity := range selection.Resources {
		if len(resources) == 0 || resources[len(resources)-1].Key() != identity.Key() {
			resources = append(resources, identity)
		}
	}
	selection.Resources = resources
	sort.Slice(selection.Fields, func(i, j int) bool {
		a, b := selection.Fields[i].Identity.Key()+"\x00"+selection.Fields[i].Path, selection.Fields[j].Identity.Key()+"\x00"+selection.Fields[j].Path
		return a < b
	})
	fields := selection.Fields[:0]
	for _, field := range selection.Fields {
		if len(fields) == 0 || fields[len(fields)-1].Identity.Key()+"\x00"+fields[len(fields)-1].Path != field.Identity.Key()+"\x00"+field.Path {
			fields = append(fields, field)
		}
	}
	selection.Fields = fields
	return selection
}

func ignoreRulesDigest(rules []store.ApplicationIgnoreRule, selectors []core.IgnoreSelector) string {
	canonical := make([]core.IgnoreRule, 0, len(rules))
	for _, rule := range rules {
		canonical = append(canonical, rule.IgnoreRule)
	}
	sort.Slice(canonical, func(i, j int) bool {
		a, b := canonical[i].Identity.Key()+"\x00"+canonical[i].Path+"\x00"+canonical[i].ID, canonical[j].Identity.Key()+"\x00"+canonical[j].Path+"\x00"+canonical[j].ID
		return a < b
	})
	encoded, _ := json.Marshal(struct {
		Rules     []core.IgnoreRule     `json:"rules"`
		Selectors []core.IgnoreSelector `json:"selectors"`
	}{canonical, selectors})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func applyResourceExclusions(plan *core.Plan, rules []store.ApplicationIgnoreRule, selection core.PlanSelection) []core.Change {
	selected := map[string]bool{}
	for _, identity := range selection.Resources {
		selected[identity.Key()] = true
	}
	ruleByIdentity := map[string]string{}
	for _, rule := range rules {
		if rule.Path == "" {
			ruleByIdentity[rule.Identity.Key()] = rule.Reason
		}
	}
	active := plan.Changes[:0]
	ignored := make([]core.Change, 0)
	for _, change := range plan.Changes {
		reason, ruleIgnored := ruleByIdentity[change.Identity.Key()]
		if ruleIgnored || selected[change.Identity.Key()] {
			if !ruleIgnored {
				reason = "Excluded from this plan's selection"
			}
			change.IgnoreReason = reason
			ignored = append(ignored, change)
			continue
		}
		active = append(active, change)
	}
	plan.Changes = active
	sort.Slice(ignored, func(i, j int) bool { return ignored[i].Identity.Key() < ignored[j].Identity.Key() })
	return ignored
}

func identityForKey(resources []core.Resource, key string) core.Identity {
	for _, resource := range resources {
		if resource.Identity.Key() == key {
			return resource.Identity
		}
	}
	return core.Identity{}
}

func ignoredPathsFor(input planInput, identity core.Identity) []string {
	if resourceExcluded(input, identity) {
		return nil
	}
	seen := map[string]bool{}
	paths := make([]string, 0)
	for _, rule := range input.IgnoreRules {
		if rule.Identity.Key() == identity.Key() && rule.Path != "" && !seen[rule.Path] {
			seen[rule.Path] = true
			paths = append(paths, rule.Path)
		}
	}
	for _, field := range input.Selection.Fields {
		if field.Identity.Key() == identity.Key() && !seen[field.Path] {
			seen[field.Path] = true
			paths = append(paths, field.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func persistentPathsFor(input planInput, identity core.Identity) []string {
	paths := make([]string, 0)
	for _, rule := range input.IgnoreRules {
		if rule.Identity.Key() == identity.Key() && rule.Path != "" {
			paths = append(paths, rule.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func selectedPathsFor(input planInput, identity core.Identity) []string {
	paths := make([]string, 0)
	for _, field := range input.Selection.Fields {
		if field.Identity.Key() == identity.Key() {
			paths = append(paths, field.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func resourceExcluded(input planInput, identity core.Identity) bool {
	if persistentResourceExcluded(input.IgnoreRules, identity) {
		return true
	}
	for _, selected := range input.Selection.Resources {
		if selected.Key() == identity.Key() {
			return true
		}
	}
	return false
}

func persistentResourceExcluded(rules []store.ApplicationIgnoreRule, identity core.Identity) bool {
	for _, rule := range rules {
		if rule.Identity.Key() == identity.Key() && rule.Path == "" {
			return true
		}
	}
	return false
}

func shouldTrackManagedResource(input planInput, resource store.ManagedResource) bool {
	return !persistentResourceExcluded(input.IgnoreRules, resource.Identity) && !selectorMatchesManifest(input.IgnoreSelectors, resource.Identity, resource.Manifest)
}

func selectorMatchesManifest(selectors []core.IgnoreSelector, identity core.Identity, manifest []byte) bool {
	var object struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(manifest, &object)
	for _, selector := range selectors {
		if selector.Matches(identity, object.Metadata.Labels) {
			return true
		}
	}
	return false
}

func (s *Service) ValidateIgnoreRule(ctx context.Context, app store.Application, identity core.Identity, path string) error {
	if identity.ClusterID == "" {
		identity.ClusterID = app.ClusterID
	}
	identity.ClusterScoped = identity.Namespace == ""
	if identity.ClusterID != app.ClusterID || identity.APIVersion == "" || identity.Kind == "" || identity.Name == "" {
		return errors.New("ignore rule must identify an exact resource in this application's cluster")
	}
	if identity.ClusterScoped {
		if app.ClusterID == "" {
			return errors.New("application cluster is unavailable")
		}
	} else {
		bound := false
		for _, binding := range app.Namespaces {
			if binding.Namespace == identity.Namespace {
				bound = true
				break
			}
		}
		if !bound {
			return errors.New("ignore rule namespace is not bound to this application")
		}
	}
	if path != "" {
		if _, err := render.ParseJSONPointer(path); err != nil {
			return err
		}
	}
	// Whole-resource exclusions must be installable before a plan exists: the
	// Kubernetes identity may be undiscoverable or unreadable by design.
	if path == "" {
		return nil
	}
	_, desired, err := s.CalculatePlan(ctx, app)
	if err != nil {
		return err
	}
	var desiredManifest []byte
	known := false
	for _, resource := range desired {
		if resource.Identity.Key() == identity.Key() {
			desiredManifest = resource.Manifest
			known = true
			break
		}
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return err
	}
	var tracked *store.ManagedResource
	for index := range managed {
		managed[index].Identity.ClusterScoped = managed[index].Identity.Namespace == ""
		if managed[index].Identity.Key() == identity.Key() {
			tracked = &managed[index]
			known = true
			break
		}
	}
	if !known {
		return errors.New("ignore rule must target a resource rendered by this application or already tracked on the cluster")
	}
	if path == "" {
		return nil
	}
	previous := []byte(nil)
	if tracked != nil {
		previous = tracked.Manifest
	}
	if !render.JSONPointerExists(desiredManifest, path) && !render.JSONPointerExists(previous, path) {
		return errors.New("field path must exist in the desired or previously managed resource")
	}
	if tracked == nil {
		return nil
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return err
	}
	mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(identity), groupVersion(identity))
	if err != nil {
		return err
	}
	client, err := clientFor(input, identity)
	if err != nil {
		return err
	}
	var resourceInterface dynamic.ResourceInterface
	if mapping.Scope.Name() == "namespace" {
		resourceInterface = client.Dynamic.Resource(mapping.Resource).Namespace(identity.Namespace)
	} else {
		resourceInterface = client.Dynamic.Resource(mapping.Resource)
	}
	object, err := resourceInterface.Get(ctx, identity.Name, v1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot verify field ownership: %w", err)
	}
	if object.GetLabels()["justcd.io/application-id"] != app.ID || string(object.GetUID()) != tracked.UID {
		return errors.New("resource ownership changed; refusing to configure an ignore rule")
	}
	if !render.HasOtherManagerFieldOwnership(object, app.ID, path) {
		return fmt.Errorf("cannot ignore %s: the field is owned only by JustCD; transfer ownership to another Kubernetes field manager first", path)
	}
	return nil
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
	canUseCluster, err := s.Store.WorkspaceCanUseCluster(ctx, app.WorkspaceID, app.ClusterID)
	if err != nil || !canUseCluster {
		return planInput{}, errors.New("application cluster is no longer available to this workspace")
	}
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return planInput{}, errors.New("application cluster is unavailable")
	}
	input := planInput{Application: app, Cluster: cluster, NamespaceBindings: map[string]store.NamespaceBinding{}, NamespaceClients: map[string]*kube.Clients{}}
	if len(app.Namespaces) == 0 {
		return planInput{}, errors.New("application has no namespace bindings")
	}
	workspaceCredentialID, err := s.Store.WorkspaceClusterCredential(ctx, app.WorkspaceID, app.ClusterID)
	if err != nil {
		return planInput{}, fmt.Errorf("load workspace cluster credential: %w", err)
	}
	for _, binding := range app.Namespaces {
		stored, err := s.Store.NamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, binding.Namespace)
		if err != nil {
			return planInput{}, fmt.Errorf("namespace %q is no longer bound to this workspace", binding.Namespace)
		}
		credentialID := stored.CredentialID
		if credentialID == nil {
			credentialID = workspaceCredentialID
		}
		if credentialID == nil {
			credentialID = cluster.DefaultCredentialID
		}
		if credentialID == nil {
			return planInput{}, fmt.Errorf("namespace %q has no Kubernetes credential", binding.Namespace)
		}
		input.Bindings = append(input.Bindings, core.Binding{ClusterID: cluster.ID, Namespace: stored.Namespace, CredentialRef: *credentialID})
		client, err := kube.ForBinding(ctx, s.Store, s.EncryptionKey, cluster, credentialID, false)
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

func (s *Service) liveSnapshot(ctx context.Context, input planInput, desired []core.Resource) ([]core.Resource, map[string][]string, error) {
	managed, err := s.Store.ManagedResources(ctx, input.Application.ID)
	if err != nil {
		return nil, nil, err
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
		// One-time selections remove changes after the live snapshot. They must
		// not make an already-managed object appear untracked during this scan.
		if !shouldTrackManagedResource(input, resource) {
			continue
		}
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
	changedIgnoredFields := map[string][]string{}
	conflicts := make([]*OwnershipConflict, 0)
	for _, key := range keys {
		identity := identities[key]
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(identity), groupVersion(identity))
		if err != nil {
			return nil, nil, fmt.Errorf("resource kind %s %s is not discoverable: %w", identity.APIVersion, identity.Kind, err)
		}
		if mapping.Scope.Name() == "namespace" && identity.ClusterScoped {
			return nil, nil, errors.New("Kubernetes discovery scope changed for a managed resource")
		}
		if mapping.Scope.Name() != "namespace" && !identity.ClusterScoped {
			return nil, nil, errors.New("Kubernetes discovery scope changed for a managed resource")
		}
		client, err := clientFor(input, identity)
		if err != nil {
			return nil, nil, err
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
					return nil, nil, err
				}
				delete(managedByKey, identity.Key())
			}
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read %s %s/%s: %w", identity.Kind, identity.Namespace, identity.Name, err)
		}
		tracked, owned := managedByKey[identity.Key()]
		if !owned {
			owner := object.GetLabels()["justcd.io/application-id"]
			ownerMissing, err := s.adoptionOwnerMissing(ctx, owner, input.Application.ID)
			if err != nil {
				return nil, nil, err
			}
			conflicts = append(conflicts, &OwnershipConflict{Identity: identity, UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), Owner: owner, OwnerMissing: ownerMissing, DesiredFingerprint: desired[desiredIndex[key]].Fingerprint, DesiredManifest: desired[desiredIndex[key]].Manifest, HasOwnerReferences: len(object.GetOwnerReferences()) > 0})
			continue
		}
		if object.GetLabels()["justcd.io/application-id"] != input.Application.ID || string(object.GetUID()) != tracked.UID {
			return nil, nil, fmt.Errorf("%s %s/%s ownership changed; refusing to adopt or mutate it", identity.Kind, identity.Namespace, identity.Name)
		}
		var resource core.Resource
		if index, exists := desiredIndex[identity.Key()]; exists {
			paths := ignoredPathsFor(input, identity)
			for _, path := range paths {
				if !render.JSONPointerExists(desired[index].Manifest, path) && !render.JSONPointerExists(previousManifests[identity.Key()], path) {
					return nil, nil, fmt.Errorf("ignored field %s is not present in the desired or previously managed manifest", path)
				}
				if !render.HasOtherManagerFieldOwnership(object, input.Application.ID, path) {
					return nil, nil, fmt.Errorf("cannot ignore %s: another Kubernetes field manager must own this field before JustCD hands it off", path)
				}
			}
			var desiredFingerprint string
			var changed []string
			resource, desiredFingerprint, changed, err = render.CanonicalLiveAgainstIgnoring(object, input.Cluster.ID, input.Application.ID, identity.ClusterScoped, desired[index].Manifest, previousManifests[identity.Key()], paths, selectedPathsFor(input, identity))
			if err == nil {
				desired[index].Fingerprint = desiredFingerprint
				if len(changed) > 0 {
					changedIgnoredFields[identity.Key()] = changed
				}
			}
		} else {
			resource, err = render.CanonicalLive(object, input.Cluster.ID, input.Application.ID, identity.ClusterScoped)
		}
		if err != nil {
			return nil, nil, err
		}
		liveByKey[identity.Key()] = resource
	}
	if len(conflicts) > 0 {
		return nil, nil, &OwnershipConflicts{Items: conflicts}
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
			return nil, nil, err
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
				return nil, nil, errors.New("cluster-scoped live discovery requires a privileged cluster-scope credential")
			}
			namespaces = []string{""}
		}
		for _, namespace := range namespaces {
			id := identity
			id.Namespace = namespace
			id.ClusterScoped = mapping.Scope.Name() != "namespace"
			client, err := clientFor(input, id)
			if err != nil {
				return nil, nil, err
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
					if apierrors.IsNotFound(err) && input.Application.CreateNamespaces && namespace != "" {
						provisioningClient, clientErr := namespaceProvisioningClient(input, namespace)
						if clientErr == nil {
							_, namespaceErr := provisioningClient.Dynamic.Resource(namespaceResource).Get(ctx, namespace, v1.GetOptions{})
							if apierrors.IsNotFound(namespaceErr) {
								break
							}
						}
					}
					return nil, nil, fmt.Errorf("cannot fully list owned %s resources: %w", identity.Kind, err)
				}
				for i := range list.Items {
					object := &list.Items[i]
					objectIdentity := core.Identity{ClusterID: input.Cluster.ID, APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), ClusterScoped: id.ClusterScoped}
					tracked, owned := managedByKey[objectIdentity.Key()]
					if !owned {
						continue
					}
					if string(object.GetUID()) != tracked.UID {
						return nil, nil, fmt.Errorf("%s %s/%s ownership changed; refusing to adopt or mutate it", objectIdentity.Kind, objectIdentity.Namespace, objectIdentity.Name)
					}
					if _, alreadyFetched := liveByKey[objectIdentity.Key()]; alreadyFetched {
						continue
					}
					var resource core.Resource
					if desiredIndexFor, exists := desiredIndex[objectIdentity.Key()]; exists {
						paths := ignoredPathsFor(input, objectIdentity)
						for _, path := range paths {
							if !render.JSONPointerExists(desired[desiredIndexFor].Manifest, path) && !render.JSONPointerExists(previousManifests[objectIdentity.Key()], path) {
								return nil, nil, fmt.Errorf("ignored field %s is not present in the desired or previously managed manifest", path)
							}
							if !render.HasOtherManagerFieldOwnership(object, input.Application.ID, path) {
								return nil, nil, fmt.Errorf("cannot ignore %s: another Kubernetes field manager must own this field before JustCD hands it off", path)
							}
						}
						var desiredFingerprint string
						var changed []string
						resource, desiredFingerprint, changed, err = render.CanonicalLiveAgainstIgnoring(object, input.Cluster.ID, input.Application.ID, id.ClusterScoped, desired[desiredIndexFor].Manifest, previousManifests[objectIdentity.Key()], paths, selectedPathsFor(input, objectIdentity))
						if err == nil {
							desired[desiredIndexFor].Fingerprint = desiredFingerprint
							if len(changed) > 0 {
								changedIgnoredFields[objectIdentity.Key()] = changed
							}
						}
					} else {
						resource, err = render.CanonicalLive(object, input.Cluster.ID, input.Application.ID, id.ClusterScoped)
					}
					if err != nil {
						return nil, nil, err
					}
					liveByKey[resource.Identity.Key()] = resource
					if len(liveByKey) > maxSnapshotResources {
						return nil, nil, errors.New("application resource snapshot exceeds 10000 resources")
					}
				}
				continueToken = list.GetContinue()
				if continueToken == "" {
					break
				}
				if page == 99 {
					return nil, nil, errors.New("resource listing exceeded page limit")
				}
			}
		}
	}
	result := make([]core.Resource, 0, len(liveByKey))
	for _, resource := range liveByKey {
		result = append(result, resource)
	}
	return result, changedIgnoredFields, nil
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
func groupVersion(identity core.Identity) string {
	gv, _ := schema.ParseGroupVersion(identity.APIVersion)
	return gv.Version
}
func groupKind(identity core.Identity) schema.GroupKind {
	gv, _ := schema.ParseGroupVersion(identity.APIVersion)
	return schema.GroupKind{Group: gv.Group, Kind: identity.Kind}
}
func kindKey(identity core.Identity) string {
	return strings.Join([]string{identity.APIVersion, identity.Kind, fmt.Sprint(identity.ClusterScoped)}, "\x00")
}
