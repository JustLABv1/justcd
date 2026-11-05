package core

import (
	"encoding/json"
	"strings"
)

// RetainResources protects stateful publishing services from pruning and app
// decommissioning. Namespace deletion would cascade to them, so it is retained too.
// Re-run for every digest to preserve protection after plan-selection changes.
func RetainResources(plan *Plan) {
	all := append(append([]Change{}, plan.Changes...), plan.Ignored...)
	protected := map[string]bool{}
	namespaces := map[string]bool{}
	for _, c := range all {
		if c.Kind != Delete {
			continue
		}
		var obj struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if json.Unmarshal(c.Before, &obj) == nil && obj.Metadata.Annotations["justcd.io/retain"] == "true" {
			protected[c.Identity.Key()] = true
			if c.Identity.Namespace != "" {
				namespaces[c.Identity.ClusterID+"/"+c.Identity.Namespace] = true
			}
		}
	}
	kept := plan.Changes[:0]
	for _, c := range plan.Changes {
		reason := ""
		if protected[c.Identity.Key()] {
			reason = "Retained application service. Delete it separately only after reviewing database and credential retention."
		}
		if c.Kind == Delete && c.Identity.Kind == "Namespace" && c.Identity.APIVersion == "v1" && namespaces[c.Identity.ClusterID+"/"+c.Identity.Name] {
			reason = "Namespace contains a retained application database or secret binding."
		}
		if reason != "" {
			c.IgnoreReason = reason
			plan.Ignored = append(plan.Ignored, c)
		} else {
			kept = append(kept, c)
		}
	}
	plan.Changes = kept
	// An ignored namespace is not automatically an opt-in deletion later.
	for i := range plan.Ignored {
		if strings.HasPrefix(plan.Ignored[i].IgnoreReason, "Namespace contains a retained") {
			plan.Selection.KeepNamespaces = true
		}
	}
}
