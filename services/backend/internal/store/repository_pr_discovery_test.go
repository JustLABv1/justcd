package store

import "testing"

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
