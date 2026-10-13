package store

import (
	"strings"
	"testing"
)

func TestRepositoryPRNamespacePatterns(t *testing.T) {
	policy := RepositoryPRSettings{Enabled: true, Mode: "review-only", Destinations: []PRDestination{{ClusterID: "dev", NamespacePattern: "dev-.*|sandbox"}}}
	if err := ValidateRepositoryPRSettings(policy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cluster, namespace string
		allowed            bool
	}{
		{"dev", "dev-shop", true},
		{"dev", "dev-payments", true},
		{"dev", "sandbox", true},
		{"prod", "dev-shop", false},
		{"dev", "production", false},
		{"dev", "production-dev-shop", false},
		{"dev", "sandbox-production", false},
		{"dev", "", false},
	} {
		if got := policy.Allows(tc.cluster, tc.namespace); got != tc.allowed {
			t.Errorf("Allows(%q, %q) = %v, want %v", tc.cluster, tc.namespace, got, tc.allowed)
		}
	}
	for _, destination := range []PRDestination{
		{ClusterID: "dev", NamespacePattern: "["},
		{ClusterID: "dev", NamespacePattern: "dev-.*", Namespace: "production"},
		{ClusterID: "dev"},
		{NamespacePattern: "dev-.*"},
		{ClusterID: "dev", NamespacePattern: strings.Repeat("a", 257)},
		{ClusterID: "dev", NamespacePattern: `(?=dev-)`},
	} {
		candidate := policy
		candidate.Destinations = []PRDestination{destination}
		if ValidateRepositoryPRSettings(candidate) == nil {
			t.Errorf("invalid destination accepted: %+v", destination)
		}
		if candidate.Allows("dev", "production") {
			t.Errorf("invalid destination granted access: %+v", destination)
		}
	}
}

func TestRepositoryPRPolicyTrustBoundary(t *testing.T) {
	base := RepositoryPRSettings{Enabled: true, Mode: "review-only", CredentialID: "credential", Destinations: []PRDestination{{ClusterID: "dev", Namespace: "app"}}}
	if err := ValidateRepositoryPRSettings(base); err != nil {
		t.Fatal(err)
	}
	if !base.Allows("dev", "app") || base.Allows("prod", "app") || base.Allows("dev", "other") {
		t.Fatal("destination matching must be exact")
	}
	for _, mutate := range []func(*RepositoryPRSettings){func(p *RepositoryPRSettings) { p.Mode = "automatic" }, func(p *RepositoryPRSettings) { p.Destinations = nil }, func(p *RepositoryPRSettings) { p.Profile.AllowForks = true }, func(p *RepositoryPRSettings) { p.Mode = "existing" }, func(p *RepositoryPRSettings) { p.Profile.ApprovalActors = map[string]string{"name": "user"} }} {
		candidate := base
		mutate(&candidate)
		if ValidateRepositoryPRSettings(candidate) == nil {
			t.Fatal("untrusted policy accepted")
		}
	}
	existing := base
	existing.Mode = "existing"
	existing.Profile.ConfirmShared = true
	if err := ValidateRepositoryPRSettings(existing); err != nil {
		t.Fatal(err)
	}
	isolated := base
	isolated.Mode = "isolated"
	isolated.Profile = PreviewProfile{NamespacePrefix: "justcd", HostSuffix: "preview.example.com", MaxActive: 10, MaxLifetimeHours: 24, QuotaCPU: "2", QuotaMemory: "4Gi", DatabaseStrategy: "none"}
	if err := ValidateRepositoryPRSettings(isolated); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryPRNamespaceCreationPolicy(t *testing.T) {
	policy := RepositoryPRSettings{Enabled: true, Mode: "existing", AllowNamespaceCreation: true, Destinations: []PRDestination{{ClusterID: "dev", NamespacePattern: "dev-.*"}}, Profile: PreviewProfile{ConfirmShared: true}}
	if err := ValidateRepositoryPRSettings(policy); err != nil {
		t.Fatal(err)
	}
	if !policy.AllowsNamespaceCreation("dev", "dev-shop") || policy.AllowsNamespaceCreation("prod", "dev-shop") || policy.AllowsNamespaceCreation("dev", "production") {
		t.Fatal("creation must stay inside the trusted destination rules")
	}
	for _, mutate := range []func(*RepositoryPRSettings){
		func(p *RepositoryPRSettings) { p.Enabled = false },
		func(p *RepositoryPRSettings) { p.AllowNamespaceCreation = false },
		func(p *RepositoryPRSettings) { p.Mode = "review-only" },
		func(p *RepositoryPRSettings) { p.Mode = "isolated" },
		func(p *RepositoryPRSettings) { p.Profile.ConfirmShared = false },
	} {
		candidate := policy
		mutate(&candidate)
		if candidate.AllowsNamespaceCreation("dev", "dev-shop") {
			t.Fatal("creation allowed by an inactive or unsupported policy")
		}
		if candidate.Enabled && candidate.AllowNamespaceCreation && ValidateRepositoryPRSettings(candidate) == nil {
			t.Fatal("creation accepted for an unsupported deployment mode")
		}
	}
	policy.Profile.ConfirmShared = false
	if ValidateRepositoryPRSettings(policy) == nil {
		t.Fatal("namespace creation bypassed shared-environment confirmation")
	}
}
