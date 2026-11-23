package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// A plan is rechecked before it is approved and again before it is applied.
// The full recheck fetches Git, renders, reads every object and dry-runs every
// update just to prove that nothing changed. The fast recheck proves the same
// from a digest of the plan's inputs:
//
//   - the commit the revision names on the remote (checked with ls-remote,
//     which also authenticates the Git credential) and the render inputs;
//   - the bindings, Kubernetes server version, ignore rules and selection;
//   - JustCD's managed-resource records;
//   - every observed object without its status, resourceVersion and field
//     manager timestamps, including field ownership, and every object
//     observed absent.
//
// Renders are deterministic for the same inputs (Helm renders are stored per
// input digest), so equal inputs produce the same desired resources. Changed
// objects must additionally keep the exact UID and resourceVersion the plan
// recorded, as the full recheck requires. Approval policy and guardrails are
// evaluated again from current settings. When anything differs or cannot be
// checked, the full recheck runs and decides.
//
// Admission webhooks and CRD schemas outside the application are not part of
// the digest. Plans expire after 15 minutes, which bounds how long such a
// change can go unnoticed by the fast path; the apply itself is still
// validated by Kubernetes.

const recheckBasisVersion = "justcd-recheck-v1"

// recheckBasis digests a plan's inputs. ok is false when an input was not
// observed, in which case the plan is only ever rechecked in full.
func recheckBasis(input planInput, commit string, managed []store.ManagedResource, desired []core.Resource) (string, bool) {
	if input.Live == nil || commit == "" || input.ReviewFingerprint == "" {
		return "", false
	}
	keys := map[string]bool{}
	for _, resource := range desired {
		keys[resource.Identity.Key()] = true
	}
	type managedRecord struct {
		Key, UID, Manifest string
	}
	records := make([]managedRecord, 0, len(managed))
	for _, resource := range managed {
		identity := resource.Identity
		identity.ClusterScoped = identity.Namespace == ""
		if !shouldTrackManagedResource(input, resource) {
			continue
		}
		keys[identity.Key()] = true
		sum := sha256.Sum256(resource.Manifest)
		records = append(records, managedRecord{Key: identity.Key(), UID: resource.UID, Manifest: hex.EncodeToString(sum[:])})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	observed := make([][2]string, 0, len(sorted))
	for _, key := range sorted {
		object, absent, known := input.Live.lookup(key)
		switch {
		case !known:
			return "", false
		case absent:
			observed = append(observed, [2]string{key, "absent"})
		default:
			digest, err := liveObjectDigest(object)
			if err != nil {
				return "", false
			}
			observed = append(observed, [2]string{key, digest})
		}
	}
	data, err := json.Marshal(struct {
		Version, Commit, Review, ServerVersion, IgnoreRules string
		Bindings                                            []core.Binding
		Selection                                           core.PlanSelection
		Managed                                             []managedRecord
		Observed                                            [][2]string
	}{recheckBasisVersion, commit, input.ReviewFingerprint, input.ServerVersion, ignoreRulesDigest(input.IgnoreRules, input.IgnoreSelectors), input.Bindings, input.Selection, records, observed})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

// liveObjectDigest hashes an object without the fields that change without
// affecting a plan: status, resourceVersion and field manager timestamps.
// Field ownership itself is kept, since it decides takeovers.
func liveObjectDigest(object *unstructured.Unstructured) (string, error) {
	copy := object.DeepCopy()
	delete(copy.Object, "status")
	unstructured.RemoveNestedField(copy.Object, "metadata", "resourceVersion")
	if entries, found, _ := unstructured.NestedSlice(copy.Object, "metadata", "managedFields"); found {
		for _, entry := range entries {
			if fields, ok := entry.(map[string]any); ok {
				delete(fields, "time")
			}
		}
		if err := unstructured.SetNestedSlice(copy.Object, entries, "metadata", "managedFields"); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(copy.Object)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// errFullRecheck reports why the fast recheck did not apply. It is never
// returned to callers; the full recheck runs instead.
type errFullRecheck struct{ reason string }

func (e errFullRecheck) Error() string { return "full recheck required: " + e.reason }

func fullRecheck(reason string) error { return errFullRecheck{reason: reason} }

// fastRecheck returns the stored plan with current approval policy and
// guardrails when its inputs are unchanged, together with the objects it
// observed. Any other outcome is an errFullRecheck or an error, and the caller
// rechecks in full.
func (s *Service) fastRecheck(ctx context.Context, app store.Application, record store.PlanRecord) (core.Plan, *liveObjects, error) {
	plan := record.Plan
	switch {
	case record.RecheckBasis == "":
		return core.Plan{}, nil, fullRecheck("plan has no recorded basis")
	case plan.Rollback != nil || plan.Decommission:
		return core.Plan{}, nil, fullRecheck("rollback and decommission plans are rechecked in full")
	case len(plan.NamespaceCreations) > 0:
		return core.Plan{}, nil, fullRecheck("namespace creation is rechecked in full")
	case app.ConfigurationMissing || app.Decommissioning:
		return core.Plan{}, nil, fullRecheck("application state changed")
	}
	rules, err := s.Store.IgnoreRules(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	selectors, err := s.Store.IgnoreSelectors(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	if ignoreRulesDigest(rules, selectors) != plan.IgnoreRulesDigest {
		return core.Plan{}, nil, fullRecheck("ignore rules changed")
	}
	source, err := s.Store.GitSourceForWorkspace(ctx, app.SourceID, app.WorkspaceID)
	if err != nil {
		return core.Plan{}, nil, fullRecheck("Git source is unavailable")
	}
	commit, found, err := gitops.ResolveRevision(ctx, s.Store, s.EncryptionKey, source, app.Revision)
	if err != nil || !found {
		return core.Plan{}, nil, fullRecheck("revision could not be resolved without fetching")
	}
	if commit != plan.Revision {
		return core.Plan{}, nil, fullRecheck("revision moved")
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return core.Plan{}, nil, fullRecheck("cluster input could not be loaded")
	}
	input.IgnoreRules, input.IgnoreSelectors, input.Selection = rules, selectors, normalizeSelection(plan.Selection)
	input.ReviewFingerprint, err = adoptionReviewFingerprint(source, app.Revision, commit, planRenderOptions(app, input, rules, selectors))
	if err != nil {
		return core.Plan{}, nil, err
	}
	for _, resource := range record.Desired {
		if resource.Identity.ClusterScoped && (input.ClusterScopeClient == nil || (app.BranchTest != nil && app.BranchTest.Mode == "isolated")) {
			return core.Plan{}, nil, fullRecheck("cluster-scoped access changed")
		}
	}
	managed, err := s.Store.ManagedResources(ctx, app.ID)
	if err != nil {
		return core.Plan{}, nil, err
	}
	identities := map[string]core.Identity{}
	for _, resource := range record.Desired {
		identities[resource.Identity.Key()] = resource.Identity
	}
	for _, resource := range managed {
		identity := resource.Identity
		identity.ClusterScoped = identity.Namespace == ""
		if shouldTrackManagedResource(input, resource) {
			identities[identity.Key()] = identity
		}
	}
	if err := observeLive(ctx, input, identities); err != nil {
		return core.Plan{}, nil, fullRecheck("live state could not be read")
	}
	basis, ok := recheckBasis(input, commit, managed, record.Desired)
	if !ok || basis != record.RecheckBasis {
		return core.Plan{}, nil, fullRecheck("inputs changed")
	}
	for _, change := range plan.Changes {
		object, absent, _ := input.Live.lookup(change.Identity.Key())
		switch change.Kind {
		case core.Create:
			if !absent {
				return core.Plan{}, nil, fullRecheck("create target appeared")
			}
		default:
			if object == nil || string(object.GetUID()) != change.LiveUID || object.GetResourceVersion() != change.LiveResourceVersion {
				return core.Plan{}, nil, fullRecheck("changed resource was modified")
			}
		}
	}
	// The reviewed changes are unchanged; approval policy and guardrails come
	// from current settings exactly as in a full plan.
	plan.Changes = append([]core.Change(nil), plan.Changes...)
	plan.Ignored = append([]core.Change(nil), plan.Ignored...)
	policy, err := s.Store.EffectiveApprovalPolicy(ctx, app)
	if err != nil {
		return core.Plan{}, nil, err
	}
	setSyncApprovalPolicy(&plan, app, policy)
	if err := s.evaluateGuardrails(ctx, app, &plan, record.Desired); err != nil {
		return core.Plan{}, nil, err
	}
	if err := core.RefreshDigest(&plan); err != nil {
		return core.Plan{}, nil, err
	}
	return plan, input.Live, nil
}

// observeLive reads every identity into input.Live, with the same owner-label
// Lists and individual reads as a live snapshot but without its bookkeeping.
func observeLive(ctx context.Context, input planInput, identities map[string]core.Identity) error {
	kinds := map[string]core.Identity{}
	for _, identity := range identities {
		kinds[kindKey(identity)] = identity
	}
	listed := map[string]*unstructured.Unstructured{}
	for _, list := range listOwnedObjects(ctx, input, kinds) {
		if list.err != nil {
			return list.err
		}
		for _, object := range list.items {
			listed[ownedObjectIdentity(input, list.id, object).Key()] = object
		}
	}
	keys := make([]string, 0, len(identities))
	for key := range identities {
		if object, ok := listed[key]; ok {
			input.Live.set(key, object)
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return forEachLimit(len(keys), input.concurrency(), func(i int) error {
		identity := identities[keys[i]]
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(identity), groupVersion(identity))
		if err != nil {
			return err
		}
		if (mapping.Scope.Name() == "namespace") == identity.ClusterScoped {
			return errors.New("Kubernetes discovery scope changed for a managed resource")
		}
		client, err := clientFor(input, identity)
		if err != nil {
			return err
		}
		var resource dynamic.ResourceInterface = client.Dynamic.Resource(mapping.Resource)
		if mapping.Scope.Name() == "namespace" {
			resource = client.Dynamic.Resource(mapping.Resource).Namespace(identity.Namespace)
		}
		object, err := resource.Get(ctx, identity.Name, v1.GetOptions{})
		if apierrors.IsNotFound(err) {
			input.Live.setAbsent(keys[i])
			return nil
		}
		if err != nil {
			return fmt.Errorf("get %s %s/%s: %w", identity.Kind, identity.Namespace, identity.Name, err)
		}
		input.Live.set(keys[i], object)
		return nil
	})
}
