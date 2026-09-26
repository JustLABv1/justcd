package syncer

import (
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestObservedDeploymentFeedsApplicationHealth(t *testing.T) {
	object := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "web", "namespace": "demo", "uid": "deployment-uid", "resourceVersion": "17"},
		"spec":       map[string]any{"replicas": int64(3)},
		"status": map[string]any{
			"readyReplicas":     int64(2),
			"updatedReplicas":   int64(3),
			"availableReplicas": int64(2),
			"conditions": []any{
				map[string]any{"type": "Available", "status": "False", "reason": "MinimumReplicasUnavailable", "message": "2 of 3 replicas are available", "lastTransitionTime": "2026-09-26T10:00:00Z"},
				map[string]any{"type": "Progressing", "status": "True", "reason": "ReplicaSetUpdated", "message": "ReplicaSet is progressing"},
			},
		},
	}}
	observed := observedObject("cluster", object)
	if observed.Identity.Kind != "Deployment" || observed.Identity.Name != "web" {
		t.Fatalf("identity = %#v", observed.Identity)
	}
	if observed.HealthSummary.ReadyReplicas == nil || *observed.HealthSummary.ReadyReplicas != 2 || observed.HealthSummary.DesiredReplicas == nil || *observed.HealthSummary.DesiredReplicas != 3 {
		t.Fatalf("replica summary = %#v", observed.HealthSummary)
	}
	if len(observed.HealthSummary.Conditions) != 2 || observed.HealthSummary.Conditions[0].LastTransitionTime == nil {
		t.Fatalf("conditions = %#v", observed.HealthSummary.Conditions)
	}
	wantTransition := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	if !observed.HealthSummary.Conditions[0].LastTransitionTime.Equal(wantTransition) {
		t.Fatalf("condition transition = %v, want %v", observed.HealthSummary.Conditions[0].LastTransitionTime, wantTransition)
	}
	condition := apphealth.Evaluate([]core.Identity{observed.Identity}, []apphealth.Observation{{
		Identity: observed.Identity, Source: observed.Source, Phase: observed.Phase, Readiness: observed.Readiness, Details: observed.HealthSummary,
	}}, nil, false, wantTransition)
	if condition.Status != apphealth.Progressing || condition.Resources[0].Reason != "DeploymentReplicasNotReady" {
		t.Fatalf("health = %#v, want progressing Deployment", condition)
	}
}

func TestObservedCrashLoopPodIsDegradedWithConditionDetails(t *testing.T) {
	object := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": "web-abc", "namespace": "demo", "uid": "pod-uid"},
		"status": map[string]any{
			"phase": "Running",
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False", "reason": "ContainersNotReady", "message": "containers with unready status: [web]"},
			},
			"containerStatuses": []any{
				map[string]any{"name": "web", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff", "message": "back-off restarting failed container"}}},
			},
		},
	}}
	observed := observedObject("cluster", object)
	if observed.Readiness != "Not ready" {
		t.Fatalf("readiness = %q", observed.Readiness)
	}
	if observed.HealthSummary.FailureReason != "CrashLoopBackOff" {
		t.Fatalf("failure = %#v", observed.HealthSummary)
	}
	condition := apphealth.Evaluate([]core.Identity{observed.Identity}, []apphealth.Observation{{
		Identity: observed.Identity, Source: observed.Source, Phase: observed.Phase, Readiness: observed.Readiness, Details: observed.HealthSummary,
	}}, nil, false, time.Now())
	if condition.Status != apphealth.Degraded || condition.Resources[0].Reason != "CrashLoopBackOff" {
		t.Fatalf("health = %#v, want CrashLoopBackOff degradation", condition)
	}
}
