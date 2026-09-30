package repoconfig

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// IgnoreResource excludes whole resources before discovery and Kubernetes reads.
// Exact resource entries use name; all other entries select either a kind or a label.
type IgnoreResource struct {
	APIVersion    string `json:"apiVersion,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Name          string `json:"name,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	ClusterScoped bool   `json:"clusterScoped,omitempty"`
	LabelKey      string `json:"labelKey,omitempty"`
	LabelValue    string `json:"labelValue,omitempty"`
	Reason        string `json:"reason"`
}

func validateIgnores(rules []IgnoreResource, namespace string) error {
	if len(rules) > 100 {
		return fmt.Errorf("ignoreResources accepts at most 100 entries")
	}
	seen := map[string]bool{}
	for i := range rules {
		rule := &rules[i]
		rule.Reason = strings.TrimSpace(rule.Reason)
		fail := func(message string) error { return fmt.Errorf("ignoreResources[%d]: %s", i, message) }
		if len(rule.Reason) < 5 || len(rule.Reason) > 500 {
			return fail("reason must contain 5–500 characters")
		}
		if rule.LabelKey != "" {
			if rule.APIVersion != "" || rule.Kind != "" || rule.Name != "" || rule.Namespace != "" || rule.ClusterScoped {
				return fail("label entries cannot also specify resource identity or scope")
			}
			if len(validation.IsQualifiedName(rule.LabelKey)) > 0 || len(validation.IsValidLabelValue(rule.LabelValue)) > 0 {
				return fail("invalid Kubernetes label key or value")
			}
		} else {
			if rule.LabelValue != "" {
				return fail("labelValue requires labelKey")
			}
			if rule.Kind == "" || rule.APIVersion == "" {
				return fail("provide apiVersion and kind, or labelKey")
			}
			parts := strings.Split(rule.APIVersion, "/")
			if len(parts) > 2 || len(validation.IsDNS1035Label(parts[len(parts)-1])) > 0 || (len(parts) == 2 && len(validation.IsDNS1123Subdomain(parts[0])) > 0) {
				return fail("invalid apiVersion")
			}
			if len(rule.Kind) > 128 || strings.ContainsAny(rule.Kind, "/ \\\t\r\n") {
				return fail("invalid kind")
			}
			if rule.Name == "" {
				if rule.Namespace != "" || rule.ClusterScoped {
					return fail("namespace and clusterScoped require an exact resource name")
				}
			} else {
				if len(validation.IsDNS1123Subdomain(rule.Name)) > 0 {
					return fail("invalid resource name")
				}
				if rule.ClusterScoped {
					if rule.Namespace != "" {
						return fail("clusterScoped resources cannot specify namespace")
					}
				} else {
					if rule.Namespace == "" {
						rule.Namespace = namespace
					}
					if rule.Namespace != namespace {
						return fail("namespace must match this application's destination")
					}
				}
			}
		}
		identity := *rule
		identity.Reason = ""
		key, _ := json.Marshal(identity)
		if seen[string(key)] {
			return fail("duplicate exclusion")
		}
		seen[string(key)] = true
	}
	return nil
}
