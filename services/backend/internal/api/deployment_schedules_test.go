package api

import (
	"sigs.k8s.io/yaml"
	"testing"
)

func TestDeploymentScheduleOpenAPISpec(t *testing.T) {
	var spec map[string]any
	if err := yaml.UnmarshalStrict([]byte(openAPISpec), &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/workspaces/{workspaceID}/deployment-schedule", "/applications/{applicationID}/deployment-schedule", "/operations/{operationID}/schedule-override", "/operations/{operationID}/cancel"} {
		if spec["paths"].(map[string]any)[path] == nil {
			t.Fatalf("missing API path %s", path)
		}
	}
	for _, name := range []string{"DeploymentWindow", "DeploymentFreeze", "DeploymentPolicy", "DeploymentSchedule"} {
		if spec["components"].(map[string]any)["schemas"].(map[string]any)[name] == nil {
			t.Fatalf("missing schema %s", name)
		}
	}
}
