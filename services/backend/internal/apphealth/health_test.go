package apphealth

import (
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func identity(kind, name string) core.Identity {
	return core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: kind, Namespace: "demo", Name: name}
}

func count(value int64) *int64 { return &value }

func TestEvaluateSupportedWorkloadStates(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		managed    []core.Identity
		observed   []Observation
		warnings   []string
		suspended  bool
		want       Status
		wantReason string
	}{
		{
			name:     "healthy deployment replicas",
			managed:  []core.Identity{identity("Deployment", "web")},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(3), ReadyReplicas: count(3), UpdatedReplicas: count(3), AvailableReplicas: count(3)}}},
			want:     Healthy, wantReason: "AllWorkloadsHealthy",
		},
		{
			name: "built-in service does not make workload health partial",
			managed: []core.Identity{
				identity("Deployment", "web"),
				{ClusterID: "cluster", APIVersion: "v1", Kind: "Service", Namespace: "demo", Name: "web"},
			},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(1), ReadyReplicas: count(1), UpdatedReplicas: count(1), AvailableReplicas: count(1)}}},
			want:     Healthy, wantReason: "AllWorkloadsHealthy",
		},
		{
			name:     "deployment rollout is incomplete",
			managed:  []core.Identity{identity("Deployment", "web")},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(3), ReadyReplicas: count(2), UpdatedReplicas: count(3), AvailableReplicas: count(2)}}},
			want:     Progressing, wantReason: "WorkloadsProgressing",
		},
		{
			name:     "deployment progress deadline failure",
			managed:  []core.Identity{identity("Deployment", "web")},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(3), ReadyReplicas: count(2), Conditions: []KubernetesCondition{{Type: "Progressing", Status: "False", Reason: "ProgressDeadlineExceeded", Message: "rollout deadline exceeded"}}}}},
			want:     Degraded, wantReason: "WorkloadDegraded",
		},
		{
			name:     "statefulset rollout is incomplete",
			managed:  []core.Identity{identity("StatefulSet", "database")},
			observed: []Observation{{Identity: identity("StatefulSet", "database"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(3), ReadyReplicas: count(2), UpdatedReplicas: count(3)}}},
			want:     Progressing, wantReason: "WorkloadsProgressing",
		},
		{
			name:     "daemonset ready on every eligible node",
			managed:  []core.Identity{identity("DaemonSet", "agent")},
			observed: []Observation{{Identity: identity("DaemonSet", "agent"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredScheduled: count(2), NumberReady: count(2), UpdatedScheduled: count(2)}}},
			want:     Healthy, wantReason: "AllWorkloadsHealthy",
		},
		{
			name:     "failed job",
			managed:  []core.Identity{identity("Job", "migrate")},
			observed: []Observation{{Identity: identity("Job", "migrate"), Source: "kubernetes", Details: ResourceDetails{Completions: count(1), Failed: count(1)}}},
			want:     Degraded, wantReason: "WorkloadDegraded",
		},
		{
			name:     "job in progress",
			managed:  []core.Identity{identity("Job", "migrate")},
			observed: []Observation{{Identity: identity("Job", "migrate"), Source: "kubernetes", Details: ResourceDetails{Completions: count(1), Active: count(1)}}},
			want:     Progressing, wantReason: "WorkloadsProgressing",
		},
		{
			name:     "completed job",
			managed:  []core.Identity{identity("Job", "migrate")},
			observed: []Observation{{Identity: identity("Job", "migrate"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, Completions: count(1), Succeeded: count(1)}}},
			want:     Healthy, wantReason: "AllWorkloadsHealthy",
		},
		{
			name:     "suspended job",
			managed:  []core.Identity{identity("Job", "migrate")},
			observed: []Observation{{Identity: identity("Job", "migrate"), Source: "kubernetes", Details: ResourceDetails{Suspended: true}}},
			want:     Suspended, wantReason: "WorkloadSuspended",
		},
		{
			name:     "failed pod",
			managed:  []core.Identity{identity("Pod", "web-1")},
			observed: []Observation{{Identity: identity("Pod", "web-1"), Source: "kubernetes", Phase: "Failed"}},
			want:     Degraded, wantReason: "WorkloadDegraded",
		},
		{
			name:    "missing managed deployment after a complete read",
			managed: []core.Identity{identity("Deployment", "web")},
			want:    Missing, wantReason: "ManagedWorkloadMissing",
		},
		{
			name:     "partial permissions never report healthy",
			managed:  []core.Identity{identity("Deployment", "web")},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(1), ReadyReplicas: count(1), UpdatedReplicas: count(1), AvailableReplicas: count(1)}}},
			warnings: []string{"deployment list forbidden"},
			want:     Partial, wantReason: "ObservationIncomplete",
		},
		{
			name:     "unavailable observation stays unknown",
			managed:  []core.Identity{identity("Deployment", "web")},
			warnings: []string{"Kubernetes resource observations are unavailable"},
			want:     Unknown, wantReason: "HealthObservationUnavailable",
		},
		{
			name:    "custom resource health is not guessed",
			managed: []core.Identity{{ClusterID: "cluster", APIVersion: "example.io/v1", Kind: "Widget", Namespace: "demo", Name: "custom"}},
			want:    Unknown, wantReason: "UnsupportedResourceHealth",
		},
		{
			name:     "mixed inferred and unsupported resources are partial",
			managed:  []core.Identity{identity("Deployment", "web"), {ClusterID: "cluster", APIVersion: "example.io/v1", Kind: "Widget", Namespace: "demo", Name: "custom"}},
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(1), ReadyReplicas: count(1), UpdatedReplicas: count(1), AvailableReplicas: count(1)}}},
			want:     Partial, wantReason: "HealthPartiallyInferred",
		},
		{
			name:      "paused reconciliation is suspended",
			managed:   []core.Identity{identity("Deployment", "web")},
			observed:  []Observation{{Identity: identity("Deployment", "web"), Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(1), ReadyReplicas: count(1), UpdatedReplicas: count(1), AvailableReplicas: count(1)}}},
			suspended: true,
			want:      Suspended, wantReason: "ReconciliationSuspended",
		},
		{
			name:     "sample fixtures never become live healthy state",
			observed: []Observation{{Identity: identity("Deployment", "web"), Source: "sample", Details: ResourceDetails{DesiredReplicas: count(1), ReadyReplicas: count(1)}}},
			want:     Unknown, wantReason: "SampleData",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Evaluate(test.managed, test.observed, test.warnings, test.suspended, now)
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q (%s)", result.Status, test.want, result.Message)
			}
			if result.Reason != test.wantReason {
				t.Fatalf("reason = %q, want %q", result.Reason, test.wantReason)
			}
			if result.ObservedAt == nil || !result.ObservedAt.Equal(now) {
				t.Fatalf("observedAt = %v, want %v", result.ObservedAt, now)
			}
		})
	}
}

func TestEvaluateSortsWarningsAndResourcesDeterministically(t *testing.T) {
	items := []Observation{
		{Identity: identity("Pod", "z-pod"), Source: "kubernetes", Phase: "Pending"},
		{Identity: identity("Pod", "a-pod"), Source: "kubernetes", Phase: "Pending"},
	}
	first := Evaluate(nil, items, []string{"second", "first", "second"}, false, time.Time{})
	second := Evaluate(nil, items, []string{"first", "second"}, false, time.Time{})
	if len(first.Resources) != 2 || first.Resources[0].Identity.Name != "a-pod" || first.Resources[1].Identity.Name != "z-pod" {
		t.Fatalf("resource order is not deterministic: %#v", first.Resources)
	}
	if len(first.Warnings) != 2 || first.Warnings[0] != "first" || first.Warnings[1] != "second" {
		t.Fatalf("warnings are not normalized: %#v", first.Warnings)
	}
	if first.Status != second.Status || first.Reason != second.Reason || first.Message != second.Message {
		t.Fatalf("same observations produced different conditions: %#v vs %#v", first, second)
	}
}

func TestEvaluateSurfacesUnassessedCustomResources(t *testing.T) {
	deployment := identity("Deployment", "web")
	custom := core.Identity{ClusterID: "cluster", APIVersion: "example.io/v1", Kind: "Widget", Namespace: "demo", Name: "custom"}
	result := Evaluate(
		[]core.Identity{deployment, custom},
		[]Observation{{Identity: deployment, Source: "kubernetes", Details: ResourceDetails{StatusObserved: true, DesiredReplicas: count(1), ReadyReplicas: count(1), UpdatedReplicas: count(1), AvailableReplicas: count(1)}}},
		nil, false, time.Now(),
	)
	if result.Status != Partial {
		t.Fatalf("status = %q, want %q", result.Status, Partial)
	}
	if len(result.Resources) != 1 || result.Resources[0].Identity.Key() != custom.Key() || result.Resources[0].Reason != "UnsupportedResourceKind" {
		t.Fatalf("custom resource assessment = %#v", result.Resources)
	}
}

func TestCustomReadinessAndConfiguration(t *testing.T) {
	custom := core.Identity{ClusterID: "cluster", APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret", Namespace: "demo", Name: "secret"}
	for _, tc := range []struct {
		name, status string
		generation   *int64
		want         Status
	}{
		{"ready", "True", count(2), Healthy},
		{"not ready", "False", count(2), Progressing},
		{"unknown", "Unknown", count(2), Unknown},
		{"stale ready", "True", count(1), Progressing},
		{"operator without generation", "True", nil, Healthy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configs := []core.Identity{custom, {APIVersion: "stackgres.io/v1", Kind: "SGInstanceProfile"}, {APIVersion: "stackgres.io/v1", Kind: "SGScript"}, {APIVersion: "v1", Kind: "ConfigMap"}, {APIVersion: "v1", Kind: "Service"}}
			item := Observation{Identity: custom, Source: "kubernetes", Details: ResourceDetails{Generation: 2, Conditions: []KubernetesCondition{{Type: "Ready", Status: tc.status, ObservedGeneration: tc.generation}}}}
			result := Evaluate(configs, []Observation{item}, nil, false, time.Now())
			if result.Status != tc.want {
				t.Fatalf("got %#v, want %s", result, tc.want)
			}
			item.Source = "sample"
			if result := Evaluate(configs, []Observation{item}, nil, false, time.Now()); result.Status == Healthy {
				t.Fatal("sample readiness must not be healthy")
			}
		})
	}
	// The same kind in another API group must not be silently exempted.
	other := core.Identity{APIVersion: "example.io/v1", Kind: "SGScript"}
	if ConfigurationResource(other) {
		t.Fatal("configuration exemption crossed API groups")
	}
}

func TestOperatorHealthEvidence(t *testing.T) {
	tests := []struct {
		name, apiVersion, kind string
		details                ResourceDetails
		want                   Status
	}{
		{"legacy vault reconciled", "secrets.hashicorp.com/v1beta1", "VaultStaticSecret", ResourceDetails{Generation: 4, LastReconciledGeneration: count(4)}, Healthy},
		{"legacy vault stale", "secrets.hashicorp.com/v1beta1", "VaultStaticSecret", ResourceDetails{Generation: 4, LastReconciledGeneration: count(3)}, Progressing},
		{"vault no evidence", "secrets.hashicorp.com/v1beta1", "VaultStaticSecret", ResourceDetails{}, Unknown},
		{"vault sync failed despite ready", "secrets.hashicorp.com/v1beta1", "VaultStaticSecret", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Ready", Status: "True"}, {Type: "SecretSynced", Status: "False", Reason: "SyncError"}}}, Degraded},
		{"stackgres inactive failure", "stackgres.io/v1", "SGCluster", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Failed", Status: "False", Reason: "FalseFailed"}}}, Unknown},
		{"stackgres bootstrapped", "stackgres.io/v1", "SGCluster", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Failed", Status: "False"}, {Type: "Bootstrapped", Status: "True"}, {Type: "InitialScriptsApplied", Status: "True"}}}, Healthy},
		{"stackgres scripts pending", "stackgres.io/v1", "SGCluster", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Bootstrapped", Status: "True"}, {Type: "InitialScriptsApplied", Status: "False"}}}, Progressing},
		{"stackgres active failure", "stackgres.io/v1", "SGCluster", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Failed", Status: "True"}}}, Degraded},
		{"stackgres stale failure", "stackgres.io/v1", "SGCluster", ResourceDetails{Generation: 4, Conditions: []KubernetesCondition{{Type: "Failed", Status: "True", ObservedGeneration: count(3)}}}, Progressing},
		{"stackgres instances pending", "stackgres.io/v1", "SGCluster", ResourceDetails{DesiredInstances: count(3), CurrentInstances: count(1), Conditions: []KubernetesCondition{{Type: "Bootstrapped", Status: "True"}}}, Progressing},
		{"stackgres stale inactive failure", "stackgres.io/v1", "SGCluster", ResourceDetails{Generation: 4, Conditions: []KubernetesCondition{{Type: "Failed", Status: "False", ObservedGeneration: count(3)}, {Type: "Bootstrapped", Status: "True", ObservedGeneration: count(4)}}}, Healthy},
		{"stackgres current failure outranks stale bootstrap", "stackgres.io/v1", "SGCluster", ResourceDetails{Generation: 4, Conditions: []KubernetesCondition{{Type: "Bootstrapped", Status: "True", ObservedGeneration: count(3)}, {Type: "Failed", Status: "True", ObservedGeneration: count(4)}}}, Degraded},
		{"stackgres restart pending", "stackgres.io/v1", "SGCluster", ResourceDetails{Conditions: []KubernetesCondition{{Type: "Bootstrapped", Status: "True"}, {Type: "PendingRestart", Status: "True"}}}, Progressing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := core.Identity{APIVersion: tt.apiVersion, Kind: tt.kind, Name: "operator"}
			observation := Observation{Identity: id, Source: "kubernetes", Details: tt.details}
			if result := Assess(observation); result.Status != tt.want {
				t.Fatalf("assessment=%#v, want %s", result, tt.want)
			}
			result := Evaluate([]core.Identity{id}, []Observation{observation}, nil, false, time.Now())
			if result.Status != tt.want {
				t.Fatalf("application=%#v, want %s", result, tt.want)
			}
		})
	}
	id := core.Identity{APIVersion: "stackgres.io/v1", Kind: "SGCluster", Name: "missing"}
	if result := Evaluate([]core.Identity{id}, nil, nil, false, time.Now()); result.Status != Missing {
		t.Fatalf("missing operator=%#v", result)
	}
}
