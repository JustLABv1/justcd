package syncer

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"testing"
)

func TestResourceActionValidation(t *testing.T) {
	zero, negative, excessive := int64(0), int64(-1), int64(10001)
	base := ResourceAction{Identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "production", Name: "api"}, UID: "uid", Action: "rescale", Replicas: &zero}
	for _, tc := range []struct {
		name   string
		change func(*ResourceAction)
		valid  bool
	}{
		{"scale to zero", func(a *ResourceAction) {}, true},
		{"negative replicas", func(a *ResourceAction) { a.Replicas = &negative }, false},
		{"excessive replicas", func(a *ResourceAction) { a.Replicas = &excessive }, false},
		{"missing replicas", func(a *ResourceAction) { a.Replicas = nil }, false},
		{"unsupported scale", func(a *ResourceAction) { a.Identity.Kind = "DaemonSet" }, false},
		{"restart daemonset", func(a *ResourceAction) { a.Action = "redeploy"; a.Identity.Kind = "DaemonSet" }, true},
		{"cluster scoped", func(a *ResourceAction) { a.Identity.Namespace = ""; a.Action = "delete" }, false},
		{"missing UID", func(a *ResourceAction) { a.UID = "" }, false},
		{"unknown action", func(a *ResourceAction) { a.Action = "exec" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.change(&a)
			if err := validateResourceAction(a); (err == nil) != tc.valid {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
