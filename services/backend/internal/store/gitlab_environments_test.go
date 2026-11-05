package store

import "testing"

func TestDefaultEnvironmentName(t *testing.T) {
	for _, tc := range []struct{ cluster, namespace, want string }{
		{"production", "shop", "production/shop"},
		{"Prod EU #1", "shop", "Prod EU -1/shop"},
		{"  ", "shop", "shop"},
	} {
		if got := DefaultEnvironmentName(tc.cluster, tc.namespace); got != tc.want {
			t.Errorf("DefaultEnvironmentName(%q,%q)=%q, want %q", tc.cluster, tc.namespace, got, tc.want)
		}
		if err := ValidateApplicationEnvironment(ApplicationEnvironment{Name: DefaultEnvironmentName(tc.cluster, tc.namespace)}); err != nil {
			t.Errorf("derived name %q must be valid: %v", tc.want, err)
		}
	}
}

func TestResolveEnvironmentAppliesOverrides(t *testing.T) {
	got := ResolveEnvironment(ApplicationEnvironment{}, "production", "shop")
	if got.Name != "production/shop" || got.Tier != "other" || got.URL != "" {
		t.Fatalf("defaults: %+v", got)
	}
	got = ResolveEnvironment(ApplicationEnvironment{Name: "shop", Tier: "production", URL: "https://shop.example.com"}, "production", "shop")
	if got.Name != "shop" || got.Tier != "production" || got.URL != "https://shop.example.com" {
		t.Fatalf("override: %+v", got)
	}
}

func TestValidateApplicationEnvironment(t *testing.T) {
	for _, bad := range []ApplicationEnvironment{
		{Name: "-bad"}, {Name: "bad-"}, {Name: "has\nnewline"}, {Tier: "prod"}, {URL: "http://shop.example.com"}, {URL: "https://user:pw@shop.example.com"}, {URL: "javascript:alert(1)"},
	} {
		if ValidateApplicationEnvironment(bad) == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
	if err := ValidateApplicationEnvironment(ApplicationEnvironment{Name: "eu/shop v2", Tier: "staging", URL: "https://shop.example.com/path"}); err != nil {
		t.Fatal(err)
	}
}
