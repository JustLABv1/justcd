package syncer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAdoptionReviewBindsSourceInputs(t *testing.T) {
	source := store.GitSource{ID: "source", RepositoryURL: "https://example.com/repo.git"}
	options := render.Options{RepositoryRoot: "/checkout/one", ManifestPath: "chart", Renderer: "helm", ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "team"}}}
	want, err := adoptionReviewFingerprint(source, "main", "commit-a", options)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*store.GitSource, *string, *string, *render.Options)
	}{
		{"commit", func(_ *store.GitSource, _ *string, commit *string, _ *render.Options) { *commit = "commit-b" }},
		{"revision", func(_ *store.GitSource, revision *string, _ *string, _ *render.Options) { *revision = "other" }},
		{"repository", func(s *store.GitSource, _, _ *string, _ *render.Options) { s.RepositoryURL += "/other" }},
		{"source", func(s *store.GitSource, _, _ *string, _ *render.Options) { s.ID = "other" }},
		{"values", func(_ *store.GitSource, _, _ *string, o *render.Options) { o.HelmValuesYAML = "replicas: 2" }},
		{"target values", func(_ *store.GitSource, _, _ *string, o *render.Options) { o.TargetHelmValuesYAML = "replicas: 2" }},
		{"namespace values", func(_ *store.GitSource, _, _ *string, o *render.Options) {
			o.NamespaceHelmValues = map[string]core.HelmValuesOverride{"team": {YAML: "replicas: 2"}}
		}},
		{"path", func(_ *store.GitSource, _, _ *string, o *render.Options) { o.ManifestPath = "other" }},
		{"renderer", func(_ *store.GitSource, _, _ *string, o *render.Options) { o.Renderer = "yaml" }},
		{"release", func(_ *store.GitSource, _, _ *string, o *render.Options) { o.HelmReleaseName = "other" }},
		{"namespace", func(_ *store.GitSource, _, _ *string, o *render.Options) {
			o.Namespaces = []core.Binding{{Namespace: "other"}}
		}},
		{"ignored resources", func(_ *store.GitSource, _, _ *string, o *render.Options) {
			o.IgnoredResources = []core.Identity{{Kind: "Secret", Name: "credentials"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, revision, commit, o := source, "main", "commit-a", options
			tc.mutate(&s, &revision, &commit, &o)
			got, err := adoptionReviewFingerprint(s, revision, commit, o)
			if err != nil || got == want {
				t.Fatalf("changed source inputs did not change review: %v", err)
			}
			fresh := OwnershipConflict{Identity: core.Identity{Kind: "Deployment", Name: "grafana"}, ReviewFingerprint: got}
			expected := fresh
			expected.ReviewFingerprint = want
			if err := validateAdoptionConflict(&fresh, &expected, "app"); !errors.Is(err, ErrAdoptionStale) {
				t.Fatalf("source change accepted: %v", err)
			}
		})
	}
	options.RepositoryRoot = "/checkout/two"
	options.Mapper = meta.NewDefaultRESTMapper(nil)
	if got, err := adoptionReviewFingerprint(source, "main", "commit-a", options); err != nil || got != want {
		t.Fatalf("checkout/discovery changed review fingerprint: %v", err)
	}
}

func TestAdoptionAllowsRandomHelmOutputForUnchangedSource(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"Chart.yaml":                "apiVersion: v2\nname: grafana\nversion: 0.1.0\n",
		"templates/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: grafana\nspec:\n  template:\n    metadata:\n      annotations:\n        checksum/secret: {{ randAlphaNum 40 | sha256sum | quote }}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gv := schema.GroupVersion{Group: "apps", Version: "v1"}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
	mapper.Add(gv.WithKind("Deployment"), meta.RESTScopeNamespace)
	options := render.Options{RepositoryRoot: root, ManifestPath: ".", Renderer: "helm", ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "team"}}, Mapper: mapper}
	fingerprint, err := adoptionReviewFingerprint(store.GitSource{ID: "source"}, "main", "commit", options)
	if err != nil {
		t.Fatal(err)
	}
	renderConflict := func() OwnershipConflict {
		t.Helper()
		resources, err := render.Render(context.Background(), options)
		if err != nil || len(resources) != 1 {
			t.Fatalf("render: %v (%d resources)", err, len(resources))
		}
		return OwnershipConflict{Identity: resources[0].Identity, UID: "uid", ResourceVersion: "27", DesiredFingerprint: resources[0].Fingerprint, DesiredManifest: resources[0].Manifest, ReviewFingerprint: fingerprint}
	}
	expected, fresh := renderConflict(), renderConflict()
	if expected.DesiredFingerprint == fresh.DesiredFingerprint {
		t.Fatal("fixture must generate different output on each render")
	}
	if _, err := validateAdoptionSelection([]*OwnershipConflict{&fresh}, []OwnershipConflict{expected}, "app"); err != nil {
		t.Fatalf("random output blocked ownership-only adoption: %v", err)
	}
	for name, mutate := range map[string]func(*OwnershipConflict){
		"replacement": func(c *OwnershipConflict) { c.UID = "other" },
		"live update": func(c *OwnershipConflict) { c.ResourceVersion = "28" },
		"owner":       func(c *OwnershipConflict) { c.Owner = "other-app" },
		"dependent":   func(c *OwnershipConflict) { c.HasOwnerReferences = true },
	} {
		t.Run(name, func(t *testing.T) {
			changed := fresh
			mutate(&changed)
			if err := validateAdoptionConflict(&changed, &expected, "app"); err == nil {
				t.Fatal("unsafe adoption accepted")
			}
		})
	}
	legacy := expected
	legacy.ReviewFingerprint = ""
	legacy.DesiredFingerprint = fresh.DesiredFingerprint
	if err := validateAdoptionConflict(&fresh, &legacy, "app"); !errors.Is(err, ErrAdoptionStale) {
		t.Fatalf("old client must refresh its review: %v", err)
	}
}

func TestValidateAdoptionConflict(t *testing.T) {
	base := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "ntfy"}, UID: "uid-1", ResourceVersion: "27", DesiredFingerprint: "git-hash"}
	if err := validateAdoptionConflict(&base, &base, "app-a"); err != nil {
		t.Fatalf("unchanged, unowned object should be claimable: %v", err)
	}
	for name, mutate := range map[string]func(*OwnershipConflict){
		"replacement":        func(c *OwnershipConflict) { c.UID = "uid-2" },
		"live change":        func(c *OwnershipConflict) { c.ResourceVersion = "28" },
		"Git change":         func(c *OwnershipConflict) { c.DesiredFingerprint = "new-git-hash" },
		"different identity": func(c *OwnershipConflict) { c.Identity.Namespace = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			fresh := base
			mutate(&fresh)
			if err := validateAdoptionConflict(&fresh, &base, "app-a"); !errors.Is(err, ErrAdoptionStale) {
				t.Fatalf("expected stale conflict, got %v", err)
			}
		})
	}
	owned := base
	owned.Owner = "app-b"
	if err := validateAdoptionConflict(&owned, &base, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("another application's label must prevent takeover: %v", err)
	}
	dependent := base
	dependent.HasOwnerReferences = true
	if err := validateAdoptionConflict(&dependent, &base, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("Kubernetes dependents must not be claimed: %v", err)
	}
}

func TestValidateAdoptionSelection(t *testing.T) {
	first := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Service", Namespace: "team", Name: "web"}, UID: "uid-1", ResourceVersion: "1", DesiredFingerprint: "git"}
	second := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "web"}, UID: "uid-2", ResourceVersion: "2", DesiredFingerprint: "git"}
	fresh := []*OwnershipConflict{&first, &second}
	if selected, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, second}, "app-a"); err != nil || len(selected) != 2 {
		t.Fatalf("two reviewed resources should be claimable: %v", err)
	}
	if _, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, first}, "app-a"); err == nil {
		t.Fatal("duplicate selection must fail before any resource is claimed")
	}
	stale := second
	stale.ResourceVersion = "3"
	if _, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, stale}, "app-a"); !errors.Is(err, ErrAdoptionStale) {
		t.Fatalf("one stale resource must reject the entire review set: %v", err)
	}
	blocked := second
	blocked.Owner = "app-b"
	if _, err := validateAdoptionSelection([]*OwnershipConflict{&first, &blocked}, []OwnershipConflict{first, second}, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("another application's resource must reject the review set: %v", err)
	}
}

func TestOwnershipConflictsUnwrapFirst(t *testing.T) {
	first := &OwnershipConflict{Identity: core.Identity{Kind: "Service", Name: "web"}}
	all := &OwnershipConflicts{Items: []*OwnershipConflict{first, {Identity: core.Identity{Kind: "Deployment", Name: "web"}}}}
	var single *OwnershipConflict
	if !errors.As(all, &single) || single != first {
		t.Fatal("existing single-conflict callers must still see the first conflict")
	}
}

func TestOnlyReviewedTakeoverStartsWithForcedApply(t *testing.T) {
	ordinary := applyPatchOptions(core.Change{Kind: core.Update}, "justcd/app-a")
	if ordinary.Force != nil {
		t.Fatal("ordinary updates must start without forced field ownership")
	}
	takeover := applyPatchOptions(core.Change{Kind: core.Update, Takeover: true}, "justcd/app-a")
	if takeover.Force == nil || !*takeover.Force {
		t.Fatal("a reviewed takeover must request field ownership transfer")
	}
}

func TestTakeoverChangesPlanDigest(t *testing.T) {
	plan := core.Plan{ApplicationID: "app-a", Revision: "commit", Changes: []core.Change{{Kind: core.Update, Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "ntfy"}}}}
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	ordinaryDigest := plan.Digest
	plan.Changes[0].Takeover = true
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest == ordinaryDigest {
		t.Fatal("approval digest must bind the ownership transfer flag")
	}
}

func TestAdoptionAllowsDeletedApplicationOwner(t *testing.T) {
	fresh := OwnershipConflict{Identity: core.Identity{Kind: "Secret", Name: "vault"}, UID: "uid", ResourceVersion: "1", DesiredFingerprint: "desired", Owner: "deleted-app", OwnerMissing: true}
	expected := fresh
	if err := validateAdoptionConflict(&fresh, &expected, "new-app"); err != nil {
		t.Fatalf("orphan label blocked: %v", err)
	}
	// Client-provided ownerMissing must never authorize an existing owner's resource.
	fresh.OwnerMissing = false
	if err := validateAdoptionConflict(&fresh, &expected, "new-app"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("client forged deletion accepted: %v", err)
	}
	fresh.OwnerMissing = true
	fresh.HasOwnerReferences = true
	if err := validateAdoptionConflict(&fresh, &expected, "new-app"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("dependent object accepted: %v", err)
	}
}

func TestAdoptionStaleExplainsWhichReviewChanged(t *testing.T) {
	base := OwnershipConflict{Identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "web"}, UID: "uid", ResourceVersion: "27", DesiredFingerprint: "git"}
	for _, tc := range []struct {
		name, detail string
		mutate       func(*OwnershipConflict)
	}{
		{"replacement", "was replaced", func(c *OwnershipConflict) { c.UID = "replacement" }},
		{"live update", "resource version 27 -> 28", func(c *OwnershipConflict) { c.ResourceVersion = "28" }},
		{"manifest update", "rendered Git manifest", func(c *OwnershipConflict) { c.DesiredFingerprint = "new-git" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := base
			tc.mutate(&fresh)
			err := validateAdoptionConflict(&fresh, &base, "app")
			if !errors.Is(err, ErrAdoptionStale) || !strings.Contains(err.Error(), tc.detail) || !strings.Contains(err.Error(), "Deployment team/web") {
				t.Fatalf("missing review diagnostics: %v", err)
			}
		})
	}
}
