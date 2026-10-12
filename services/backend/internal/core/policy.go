package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"
)

// PolicyConfig is workspace/project scoped. All built-ins are enabled by default.
// Audit records findings, warn also highlights them, and block prevents deployment.
type PolicyConfig struct {
	Mode                string   `json:"mode"`
	ForbiddenNamespaces []string `json:"forbiddenNamespaces,omitempty"`
	DisabledRules       []string `json:"disabledRules,omitempty"`
}

type PolicyResult struct {
	Resource    Identity `json:"resource"`
	Rule        string   `json:"rule"`
	Evidence    string   `json:"evidence"`
	Remediation string   `json:"remediation"`
}

type PolicyEvaluation struct {
	ConfigDigest string         `json:"configDigest"`
	Mode         string         `json:"mode"`
	Results      []PolicyResult `json:"results"`
}

var BuiltinPolicyRules = []string{"privileged-containers", "mutable-image-tags", "missing-resource-limits", "public-services", "forbidden-namespaces"}

func NormalizePolicyConfig(c PolicyConfig) (PolicyConfig, error) {
	if c.Mode == "" {
		c.Mode = "audit"
	}
	if c.Mode != "audit" && c.Mode != "warn" && c.Mode != "block" {
		return c, errors.New("policy mode must be audit, warn, or block")
	}
	if len(c.ForbiddenNamespaces) > 100 || len(c.DisabledRules) > len(BuiltinPolicyRules) {
		return c, errors.New("too many policy configuration entries")
	}
	for _, rule := range c.DisabledRules {
		if !slices.Contains(BuiltinPolicyRules, rule) {
			return c, fmt.Errorf("unknown built-in policy rule %q", rule)
		}
	}
	for _, namespace := range c.ForbiddenNamespaces {
		if !namespacePattern.MatchString(namespace) || len(namespace) > 63 {
			return c, errors.New("forbidden namespaces must be valid Kubernetes namespace names")
		}
	}
	c.ForbiddenNamespaces = slices.Clone(c.ForbiddenNamespaces)
	c.DisabledRules = slices.Clone(c.DisabledRules)
	slices.Sort(c.ForbiddenNamespaces)
	c.ForbiddenNamespaces = slices.Compact(c.ForbiddenNamespaces)
	slices.Sort(c.DisabledRules)
	c.DisabledRules = slices.Compact(c.DisabledRules)
	return c, nil
}

var namespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
var immutableImage = regexp.MustCompile(`^.+@sha256:[a-fA-F0-9]{64}$`)

func PolicyConfigDigest(c PolicyConfig) (string, error) {
	c, err := NormalizePolicyConfig(c)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// PolicyEngine is the extension boundary for future CEL or OPA/Rego adapters.
// Engines receive only desired resource manifests and configuration: no store,
// credentials, cluster client, or mutation capabilities. Adapters must be local,
// deterministic, and fail closed; they must never enable network/data built-ins.
type PolicyEngine interface {
	Evaluate(PolicyConfig, []Resource) (PolicyEvaluation, error)
}

type BuiltinPolicyEngine struct{}

func (BuiltinPolicyEngine) Evaluate(config PolicyConfig, resources []Resource) (PolicyEvaluation, error) {
	config, err := NormalizePolicyConfig(config)
	if err != nil {
		return PolicyEvaluation{}, err
	}
	digest, err := PolicyConfigDigest(config)
	if err != nil {
		return PolicyEvaluation{}, err
	}
	out := PolicyEvaluation{ConfigDigest: digest, Mode: config.Mode, Results: []PolicyResult{}}
	resources = slices.Clone(resources)
	slices.SortFunc(resources, func(a, b Resource) int {
		if a.Identity.Key() < b.Identity.Key() {
			return -1
		}
		if a.Identity.Key() > b.Identity.Key() {
			return 1
		}
		return 0
	})
	for _, r := range resources {
		add := func(rule, evidence, remediation string) {
			if !slices.Contains(config.DisabledRules, rule) {
				out.Results = append(out.Results, PolicyResult{r.Identity, rule, evidence, remediation})
			}
		}
		namespace := r.Identity.Namespace
		if r.Identity.Kind == "Namespace" && r.Identity.APIVersion == "v1" {
			namespace = r.Identity.Name
		}
		if slices.Contains(config.ForbiddenNamespaces, namespace) {
			add("forbidden-namespaces", "Resource targets forbidden namespace "+namespace, "Choose an allowed namespace or ask a project owner to change the policy.")
		}
		var manifest map[string]any
		if err := json.Unmarshal(r.Manifest, &manifest); err != nil || manifest == nil {
			return PolicyEvaluation{}, errors.New("policy evaluation requires a valid desired resource manifest")
		}
		spec := object(manifest, "spec")
		if r.Identity.Kind == "Service" && r.Identity.APIVersion == "v1" {
			typ, _ := spec["type"].(string)
			external, _ := spec["externalIPs"].([]any)
			if typ == "LoadBalancer" || typ == "NodePort" || len(external) > 0 {
				add("public-services", "Service exposes traffic through "+typ+" or externalIPs", "Use ClusterIP and remove externalIPs, or disable this rule if public exposure is intended.")
			}
		}
		var pod map[string]any
		switch {
		case r.Identity.Kind == "Pod" && r.Identity.APIVersion == "v1":
			pod = spec
		case slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"}, r.Identity.Kind) && r.Identity.APIVersion == "apps/v1":
			pod = object(object(spec, "template"), "spec")
		case r.Identity.Kind == "ReplicationController" && r.Identity.APIVersion == "v1":
			pod = object(object(spec, "template"), "spec")
		case r.Identity.Kind == "Job" && r.Identity.APIVersion == "batch/v1":
			pod = object(object(spec, "template"), "spec")
		case r.Identity.Kind == "CronJob" && r.Identity.APIVersion == "batch/v1":
			pod = object(object(object(object(spec, "jobTemplate"), "spec"), "template"), "spec")
		}
		if pod == nil {
			continue
		}
		for _, field := range []string{"hostNetwork", "hostPID", "hostIPC"} {
			if pod[field] == true {
				add("privileged-containers", "Pod spec enables "+field, "Disable host network/process/IPC access.")
			}
		}
		for _, group := range []string{"containers", "initContainers", "ephemeralContainers"} {
			containers, _ := pod[group].([]any)
			for i, value := range containers {
				container, ok := value.(map[string]any)
				if !ok {
					return PolicyEvaluation{}, errors.New("invalid container in policy input")
				}
				// Evidence contains field paths rather than values; Secret data/env values
				// and credentials are never copied into findings or audit events.
				path := fmt.Sprintf("%s[%d]", group, i)
				if object(container, "securityContext")["privileged"] == true {
					add("privileged-containers", path+".securityContext.privileged is true", "Set privileged to false and grant only required capabilities.")
				}
				image, _ := container["image"].(string)
				if !immutableImage.MatchString(image) {
					add("mutable-image-tags", path+".image is not pinned to a SHA-256 digest", "Pin the image using image@sha256:<64 hexadecimal characters>.")
				}
				// Ephemeral containers cannot declare resources in Kubernetes.
				if group != "ephemeralContainers" {
					limits := object(object(container, "resources"), "limits")
					for _, field := range []string{"cpu", "memory"} {
						raw := fmt.Sprint(limits[field])
						quantity, err := resource.ParseQuantity(raw)
						if err != nil || quantity.Sign() <= 0 {
							add("missing-resource-limits", path+".resources.limits."+field+" is missing or non-positive", "Set positive CPU and memory limits for every regular and init container.")
						}
					}
				}
			}
		}
	}
	return out, nil
}

func object(m map[string]any, key string) map[string]any {
	value, _ := m[key].(map[string]any)
	return value
}

func (p *PolicyEvaluation) Authorize() error {
	if p != nil && p.Mode == "block" && len(p.Results) > 0 {
		return errors.New("deployment blocked by project policy; fix the violations and create a new plan")
	}
	return nil
}
