package api

import "testing"

func TestBuildOnboardingResponseIsDeterministic(t *testing.T) {
	snapshot := onboardingSnapshot{
		DatabaseReady: true, EncryptionAcknowledged: true, PublicURLReady: true,
		WorkspaceCount: 1, KubernetesCredentialCount: 1, ClusterCount: 1,
		NamespaceBindingCount: 1, GitSourceCount: 1, ApplicationCount: 1,
		FirstWorkspaceID: "workspace-1", ClusterStatus: "passed",
	}
	first := buildOnboardingResponse(snapshot)
	second := buildOnboardingResponse(snapshot)
	if first.Completed != 7 || first.Total != 7 || first.NextStepID != "" {
		t.Fatalf("unexpected completed response: %#v", first)
	}
	for index := range first.Steps {
		if first.Steps[index] != second.Steps[index] {
			t.Fatalf("step %d changed between identical snapshots", index)
		}
	}
}

func TestBuildOnboardingResponseSelectsFirstRequiredStep(t *testing.T) {
	response := buildOnboardingResponse(onboardingSnapshot{DatabaseReady: true})
	if response.NextStepID != "encryption-key" {
		t.Fatalf("next step = %q, want encryption-key", response.NextStepID)
	}
	if response.Steps[4].Status != "action_required" || response.Steps[4].Category != "configuration" {
		t.Fatalf("unexpected cluster step: %#v", response.Steps[4])
	}
}

func TestBuildOnboardingResponseClassifiesClusterFailure(t *testing.T) {
	response := buildOnboardingResponse(onboardingSnapshot{
		DatabaseReady: true, EncryptionAcknowledged: true, PublicURLReady: true,
		WorkspaceCount: 1, KubernetesCredentialCount: 1, ClusterCount: 1,
		NamespaceBindingCount: 1, ClusterStatus: "failed",
		ClusterCategory: "authorization", ClusterMessage: "permission review denied",
		ClusterRemediation: "Grant SelfSubjectAccessReview access.",
	})
	step := response.Steps[4]
	if step.Status != "failed" || step.Category != "permission" || step.Summary != "permission review denied" {
		t.Fatalf("unexpected failed cluster step: %#v", step)
	}
}

func TestClusterConnectionHrefAdvancesThroughWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		snapshot onboardingSnapshot
		want     string
	}{
		{name: "workspace", snapshot: onboardingSnapshot{}, want: "/workspaces/new"},
		{name: "credential", snapshot: onboardingSnapshot{FirstWorkspaceID: "p1"}, want: "/workspaces/p1/clusters/new"},
		{name: "cluster", snapshot: onboardingSnapshot{FirstWorkspaceID: "p1", KubernetesCredentialCount: 1}, want: "/workspaces/p1/clusters/new"},
		{name: "namespace", snapshot: onboardingSnapshot{FirstWorkspaceID: "p1", KubernetesCredentialCount: 1, ClusterCount: 1}, want: "/workspaces/p1/clusters/new"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clusterConnectionHref(test.snapshot); got != test.want {
				t.Fatalf("href = %q, want %q", got, test.want)
			}
		})
	}
}
