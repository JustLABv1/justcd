package api

import (
	"testing"
	"time"
)

func TestClusterConnectionStateForAgents(t *testing.T) {
	now := time.Now()
	recent, stale := now.Add(-20*time.Second), now.Add(-time.Hour)
	tests := []struct {
		name             string
		revoked, pending bool
		lastSeen         *time.Time
		want             string
	}{
		{"not enrolled", false, false, nil, "not_enrolled"},
		{"waiting for the agent", false, true, nil, "waiting"},
		{"online", false, false, &recent, "online"},
		{"online even with a leftover token", false, true, &recent, "online"},
		{"offline", false, false, &stale, "offline"},
		{"revoked beats a recent heartbeat", true, false, &recent, "revoked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, message := clusterConnectionState(true, test.revoked, test.pending, test.lastSeen, "", "", now)
			if got != test.want || message == "" {
				t.Fatalf("state = %q (%q), want %q", got, message, test.want)
			}
		})
	}
}

func TestClusterConnectionStateForDirectClusters(t *testing.T) {
	now := time.Now()
	for status, want := range map[string]string{"": "unverified", "passed": "verified", "partial": "partial", "failed": "failed"} {
		if got, _ := clusterConnectionState(false, false, false, nil, status, "", now); got != want {
			t.Fatalf("status %q -> %q, want %q", status, got, want)
		}
	}
	if _, message := clusterConnectionState(false, false, false, nil, "failed", "permission review denied", now); message != "permission review denied" {
		t.Fatalf("failure message = %q", message)
	}
}

// An agent-connected cluster has no credential, namespace binding or permission test; it must still complete the step.
func TestOnlineAgentCompletesClusterStep(t *testing.T) {
	response := buildOnboardingResponse(onboardingSnapshot{
		DatabaseReady: true, EncryptionAcknowledged: true, PublicURLReady: true,
		WorkspaceID: "w1", WorkspaceCount: 1,
		Clusters: []onboardingCluster{{ID: "c1", Name: "m025-1", Mode: "agent", State: "online"}},
	})
	cluster := response.Steps[1]
	if cluster.ID != "cluster" || cluster.Status != "complete" || cluster.Summary != "m025-1 is connected." {
		t.Fatalf("cluster step = %#v", cluster)
	}
	if response.NextStepID != "git" || response.Completed != 2 {
		t.Fatalf("next = %q completed = %d", response.NextStepID, response.Completed)
	}
}

func TestClusterStepFollowsTheMostUsefulCluster(t *testing.T) {
	waiting := onboardingCluster{Name: "edge", Mode: "agent", State: "waiting", Message: "Waiting for the agent to enroll with the one-use token."}
	failed := onboardingCluster{Name: "prod", Mode: "direct", State: "failed", Message: "permission review denied"}
	base := onboardingSnapshot{WorkspaceID: "w1", WorkspaceCount: 1}

	if step := buildOnboardingResponse(base).Steps[1]; step.Status != "action_required" || step.Href != "/workspaces/w1/clusters/new" {
		t.Fatalf("no clusters = %#v", step)
	}
	base.Clusters = []onboardingCluster{waiting}
	if step := buildOnboardingResponse(base).Steps[1]; step.Status != "action_required" || step.Summary != "edge: "+waiting.Message {
		t.Fatalf("waiting = %#v", step)
	}
	base.Clusters = []onboardingCluster{failed}
	if step := buildOnboardingResponse(base).Steps[1]; step.Status != "failed" || step.Category != "connectivity" {
		t.Fatalf("failed = %#v", step)
	}
	base.Clusters = []onboardingCluster{failed, {Name: "edge", Mode: "agent", State: "online"}}
	if step := buildOnboardingResponse(base).Steps[1]; step.Status != "complete" {
		t.Fatalf("one ready cluster must win over a failed one: %#v", step)
	}
}

// A partly verified direct connection works; it must not leave the step red or pending.
func TestPartiallyVerifiedClusterCompletesClusterStep(t *testing.T) {
	step := buildOnboardingResponse(onboardingSnapshot{WorkspaceID: "w1", WorkspaceCount: 1, Clusters: []onboardingCluster{{Name: "m025-1", Mode: "direct", State: "partial"}}}).Steps[1]
	if step.Status != "complete" {
		t.Fatalf("cluster step = %#v", step)
	}
}

func TestSystemChecksDoNotBlockGuidedProgress(t *testing.T) {
	response := buildOnboardingResponse(onboardingSnapshot{WorkspaceID: "w1", WorkspaceCount: 1, GitSourceCount: 1, ApplicationCount: 1, Clusters: []onboardingCluster{{Name: "a", State: "verified"}}})
	if response.Completed != response.Total || response.NextStepID != "" {
		t.Fatalf("guided steps = %#v", response)
	}
	if response.SystemAttention != 3 || len(response.System) != 3 {
		t.Fatalf("system attention = %d of %d", response.SystemAttention, len(response.System))
	}
}

func TestBuildOnboardingResponseIsDeterministic(t *testing.T) {
	snapshot := onboardingSnapshot{DatabaseReady: true, EncryptionAcknowledged: true, PublicURLReady: true, WorkspaceID: "w1", WorkspaceCount: 1, GitSourceCount: 1, ApplicationCount: 1, Clusters: []onboardingCluster{{Name: "a", State: "online"}}}
	first, second := buildOnboardingResponse(snapshot), buildOnboardingResponse(snapshot)
	if first.Completed != 4 || first.Total != 4 || first.SystemAttention != 0 {
		t.Fatalf("unexpected completed response: %#v", first)
	}
	for index := range first.Steps {
		if first.Steps[index] != second.Steps[index] {
			t.Fatalf("step %d changed between identical snapshots", index)
		}
	}
}

func TestOnboardingLinksFollowTheWorkspace(t *testing.T) {
	if got := clusterConnectionHref(onboardingSnapshot{}); got != "/workspaces/new" {
		t.Fatalf("no workspace href = %q", got)
	}
	if got := gitHref(onboardingSnapshot{WorkspaceID: "p1"}); got != "/workspaces/p1/connections/git-sources" {
		t.Fatalf("git href = %q", got)
	}
}
