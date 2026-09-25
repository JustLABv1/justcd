package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"
)

// Binding ties one namespace to one credential reference. Secrets stay outside plans.
type Binding struct {
	ClusterID     string `json:"clusterId"`
	Namespace     string `json:"namespace"`
	CredentialRef string `json:"credentialRef"`
	ClusterScope  bool   `json:"clusterScope,omitempty"`
}

type Identity struct {
	ClusterID     string `json:"clusterId,omitempty"`
	APIVersion    string `json:"apiVersion"`
	Kind          string `json:"kind"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ClusterScoped bool   `json:"clusterScoped,omitempty"`
}

func (id Identity) key() string {
	return id.ClusterID + "\x00" + id.APIVersion + "\x00" + id.Kind + "\x00" + id.Namespace + "\x00" + id.Name
}

// Key returns a stable, collision-safe key for this Kubernetes identity.
func (id Identity) Key() string { return id.key() }

func (id Identity) valid() bool {
	if id.APIVersion == "" || id.Kind == "" || id.Name == "" {
		return false
	}
	return id.ClusterScoped == (id.Namespace == "")
}

// Resource is a normalized manifest or live object. Fingerprint is a canonical
// hash of the fields JustCD manages; UID is populated only for live objects.
type Resource struct {
	Identity        Identity        `json:"identity"`
	Fingerprint     string          `json:"fingerprint"`
	UID             string          `json:"uid,omitempty"`
	ResourceVersion string          `json:"resourceVersion,omitempty"`
	Owner           string          `json:"owner,omitempty"`
	Manifest        json.RawMessage `json:"manifest,omitempty"`
}

type ChangeKind string

const (
	Create ChangeKind = "create"
	Update ChangeKind = "update"
	Delete ChangeKind = "delete"
)

type Change struct {
	Kind                ChangeKind      `json:"kind"`
	Takeover            bool            `json:"takeover,omitempty"`
	Identity            Identity        `json:"identity"`
	LiveUID             string          `json:"liveUid,omitempty"`
	LiveResourceVersion string          `json:"liveResourceVersion,omitempty"`
	DesiredFingerprint  string          `json:"desiredFingerprint,omitempty"`
	LiveFingerprint     string          `json:"liveFingerprint,omitempty"`
	Before              json.RawMessage `json:"before,omitempty"`
	After               json.RawMessage `json:"after,omitempty"`
	ChangedPaths        []string        `json:"changedPaths,omitempty"`
	IgnoredPaths        []string        `json:"ignoredPaths,omitempty"`
	IgnoreReason        string          `json:"ignoreReason,omitempty"`
}

type FieldExclusion struct {
	Identity Identity `json:"identity"`
	Path     string   `json:"path"`
}

type PlanSelection struct {
	Resources []Identity       `json:"resources,omitempty"`
	Fields    []FieldExclusion `json:"fields,omitempty"`
}

type IgnoreRule struct {
	ID       string   `json:"id"`
	Identity Identity `json:"identity"`
	Path     string   `json:"path,omitempty"`
	Reason   string   `json:"reason"`
}

// IgnoreSelector excludes a rendered resource by exact GVK or label before
// discovery/live reads. It is scoped to one application and audited separately.
type IgnoreSelector struct {
	ID         string    `json:"id"`
	APIVersion string    `json:"apiVersion,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	LabelKey   string    `json:"labelKey,omitempty"`
	LabelValue string    `json:"labelValue,omitempty"`
	Reason     string    `json:"reason"`
	CreatedBy  string    `json:"createdBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
}

func (selector IgnoreSelector) Matches(identity Identity, labels map[string]string) bool {
	if selector.Kind != "" && (selector.Kind != identity.Kind || selector.APIVersion != identity.APIVersion) {
		return false
	}
	if selector.LabelKey != "" {
		value, exists := labels[selector.LabelKey]
		if !exists || value != selector.LabelValue {
			return false
		}
	}
	return selector.Kind != "" || selector.LabelKey != ""
}

type Plan struct {
	ApplicationID     string        `json:"applicationId"`
	Decommission      bool          `json:"decommission,omitempty"`
	Revision          string        `json:"revision"`
	Bindings          []Binding     `json:"bindings"`
	Changes           []Change      `json:"changes"`
	Ignored           []Change      `json:"ignored,omitempty"`
	Selection         PlanSelection `json:"selection,omitempty"`
	IgnoreRulesDigest string        `json:"ignoreRulesDigest,omitempty"`
	RequiresApproval  bool          `json:"requiresApproval"`
	ApprovalKind      string        `json:"approvalKind,omitempty"`
	RequiredApprovals int           `json:"requiredApprovals,omitempty"`
	ApproverRoles     []string      `json:"approverRoles,omitempty"`
	ApproverUserIDs   []string      `json:"approverUserIds,omitempty"`
	Digest            string        `json:"digest"`
}

// BuildPlan is pure: callers must provide a complete, authorized live snapshot
// for all managed resource kinds. An incomplete snapshot must fail before this call.
func BuildPlan(applicationID, revision string, bindings []Binding, desired, live []Resource) (Plan, error) {
	if applicationID == "" || revision == "" || len(bindings) == 0 {
		return Plan{}, errors.New("application, revision, and bindings are required")
	}
	allowed := make(map[string]struct{}, len(bindings))
	clusters := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.ClusterID == "" || binding.CredentialRef == "" || (binding.ClusterScope && binding.Namespace != "") || (!binding.ClusterScope && binding.Namespace == "") {
			return Plan{}, errors.New("namespace bindings require cluster, namespace, and credential reference; cluster-scope bindings require cluster and credential reference")
		}
		key := binding.ClusterID + "\x00" + binding.Namespace
		if binding.ClusterScope {
			key = binding.ClusterID + "\x00*"
		}
		if _, exists := allowed[key]; exists {
			return Plan{}, fmt.Errorf("duplicate binding %q", key)
		}
		allowed[key] = struct{}{}
		clusters[binding.ClusterID] = struct{}{}
	}
	defaultCluster := ""
	if len(clusters) == 1 {
		for clusterID := range clusters {
			defaultCluster = clusterID
		}
	}
	index := make(map[string]Resource, len(live))
	for _, resource := range live {
		identity := resource.Identity
		if identity.ClusterID == "" {
			identity.ClusterID = defaultCluster
		}
		if !identity.valid() || resource.Fingerprint == "" || resource.UID == "" {
			return Plan{}, errors.New("live resource requires identity, fingerprint, and UID")
		}
		resource.Identity = identity
		if !resourceInBindings(identity, allowed) {
			return Plan{}, fmt.Errorf("live resource outside bound target %q", identity.Namespace)
		}
		key := identity.key()
		if _, exists := index[key]; exists {
			return Plan{}, fmt.Errorf("duplicate live resource %q", key)
		}
		index[key] = resource
	}
	seen := make(map[string]struct{}, len(desired))
	changes := make([]Change, 0)
	for _, resource := range desired {
		identity := resource.Identity
		if identity.ClusterID == "" {
			identity.ClusterID = defaultCluster
		}
		if !identity.valid() || resource.Fingerprint == "" {
			return Plan{}, errors.New("desired resource requires identity and fingerprint")
		}
		resource.Identity = identity
		if !resourceInBindings(identity, allowed) {
			return Plan{}, fmt.Errorf("desired resource outside bound target %q", identity.Namespace)
		}
		key := identity.key()
		if _, exists := seen[key]; exists {
			return Plan{}, fmt.Errorf("duplicate desired resource %q", key)
		}
		seen[key] = struct{}{}
		current, exists := index[key]
		if !exists {
			changes = append(changes, Change{Kind: Create, Identity: identity, DesiredFingerprint: resource.Fingerprint, After: resource.Manifest})
		} else if current.Fingerprint != resource.Fingerprint {
			changes = append(changes, Change{Kind: Update, Identity: identity, LiveUID: current.UID, LiveResourceVersion: current.ResourceVersion, DesiredFingerprint: resource.Fingerprint, LiveFingerprint: current.Fingerprint, Before: current.Manifest, After: resource.Manifest})
		}
	}
	for key, resource := range index {
		if resource.Owner != applicationID {
			continue
		}
		if _, exists := seen[key]; !exists {
			changes = append(changes, Change{Kind: Delete, Identity: resource.Identity, LiveUID: resource.UID, LiveResourceVersion: resource.ResourceVersion, LiveFingerprint: resource.Fingerprint, Before: resource.Manifest})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int {
		if a.Identity.key() < b.Identity.key() {
			return -1
		}
		if a.Identity.key() > b.Identity.key() {
			return 1
		}
		return 0
	})
	bindings = slices.Clone(bindings)
	slices.SortFunc(bindings, func(a, b Binding) int {
		ak := a.ClusterID + "\x00" + a.Namespace + "\x00" + fmt.Sprint(a.ClusterScope)
		bk := b.ClusterID + "\x00" + b.Namespace + "\x00" + fmt.Sprint(b.ClusterScope)
		if ak < bk {
			return -1
		}
		if ak > bk {
			return 1
		}
		return 0
	})
	requiresApproval := false
	for _, change := range changes {
		if change.Kind == Delete || change.Identity.ClusterScoped {
			requiresApproval = true
			break
		}
	}
	plan := Plan{ApplicationID: applicationID, Revision: revision, Bindings: bindings, Changes: changes, RequiresApproval: requiresApproval}
	if err := RefreshDigest(&plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func RefreshDigest(plan *Plan) error {
	plan.Digest = ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	plan.Digest = hex.EncodeToString(sum[:])
	return nil
}

func resourceInBindings(identity Identity, allowed map[string]struct{}) bool {
	key := identity.ClusterID + "\x00" + identity.Namespace
	if identity.ClusterScoped {
		key = identity.ClusterID + "\x00*"
	}
	_, ok := allowed[key]
	return ok
}

type DeletionApproval struct {
	PlanDigest string    `json:"planDigest"`
	ActorID    string    `json:"actorId"`
	Deletes    []Change  `json:"deletes"`
	Privileged []Change  `json:"privileged,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// AuthorizeApply preserves the single-approval entry point for callers that
// have not migrated to policy-based approval lists. The executor must also
// verify plan freshness, membership and resource UID before mutating anything.
func AuthorizeApply(plan Plan, approval *DeletionApproval, now time.Time) error {
	if approval == nil {
		return AuthorizeApplyMany(plan, nil, now)
	}
	return AuthorizeApplyMany(plan, []DeletionApproval{*approval}, now)
}

// RequiredApprovalCount preserves the one-owner approval behavior for plans
// written before approval policies were added.
func RequiredApprovalCount(plan Plan) int {
	if plan.RequiredApprovals > 0 {
		return plan.RequiredApprovals
	}
	if plan.RequiresApproval {
		return 1
	}
	return 0
}

// ApprovalRoleAllows checks the snapshotted project role or explicit member
// list for a plan. A higher project role satisfies a lower role requirement.
func ApprovalRoleAllows(plan Plan, role, userID string) bool {
	for _, selected := range plan.ApproverUserIDs {
		if selected != "" && selected == userID {
			return true
		}
	}
	rank := func(value string) int {
		switch value {
		case "owner":
			return 3
		case "deployer":
			return 2
		case "viewer":
			return 1
		default:
			return 0
		}
	}
	if len(plan.ApproverRoles) == 0 && len(plan.ApproverUserIDs) == 0 && RequiredApprovalCount(plan) > 0 {
		return role == "owner"
	}
	for _, allowed := range plan.ApproverRoles {
		if rank(role) > 0 && rank(role) >= rank(allowed) {
			return true
		}
	}
	return false
}

// AuthorizeApplyMany rejects plans whose exact change set lacks the required
// number of distinct, current approvals.
func AuthorizeApplyMany(plan Plan, approvals []DeletionApproval, now time.Time) error {
	var deletes []Change
	var privileged []Change
	for _, change := range plan.Changes {
		if change.Kind == Delete {
			deletes = append(deletes, change)
		}
		if change.Identity.ClusterScoped {
			privileged = append(privileged, change)
		}
	}
	required := RequiredApprovalCount(plan)
	if required == 0 && len(deletes) == 0 && len(privileged) == 0 {
		return nil
	}
	if required == 0 {
		required = 1
	}
	if len(approvals) < required {
		return fmt.Errorf("plan requires %d current approval(s)", required)
	}
	actors := make(map[string]struct{}, len(approvals))
	for _, approval := range approvals {
		if approval.ActorID == "" || approval.PlanDigest != plan.Digest || !now.Before(approval.ExpiresAt) {
			return errors.New("approval is expired or does not match this plan")
		}
		if _, exists := actors[approval.ActorID]; exists {
			return errors.New("approvals must come from distinct project members")
		}
		actors[approval.ActorID] = struct{}{}
		if len(deletes) != len(approval.Deletes) || (len(deletes) > 0 && !reflect.DeepEqual(deletes, approval.Deletes)) {
			return errors.New("approval does not match the exact deletion set")
		}
		if len(privileged) != len(approval.Privileged) || (len(privileged) > 0 && !reflect.DeepEqual(privileged, approval.Privileged)) {
			return errors.New("approval does not match the exact cluster-scoped change set")
		}
	}
	return nil
}
