package publishing

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Rendered template output is untrusted: whatever an administrator wrote, the
// manifest must keep the workload's security posture and may not publish
// anything outside the application's own namespace and host.

const (
	maxResources = 20
	appContainer = "app"
)

type resource struct {
	kind string
	name string
	body map[string]any
}

var (
	extraKinds     = map[string]string{"ConfigMap": "v1", "NetworkPolicy": "networking.k8s.io/v1", "PodDisruptionBudget": "policy/v1", "HorizontalPodAutoscaler": "autoscaling/v2"}
	generatedKinds = map[string]string{"Deployment": "apps/v1", "Service": "v1", "Route": "route.openshift.io/v1", "Ingress": "networking.k8s.io/v1"}
	allowedVolumes = map[string]bool{"name": true, "emptyDir": true, "configMap": true, "secret": true, "projected": true, "downwardAPI": true, "persistentVolumeClaim": true}
	documentSplit  = regexp.MustCompile(`(?m)^---[ \t]*$`)
)

// ValidateRendered checks the rendered files of one configuration.
func ValidateRendered(c Config, out map[string]string) error {
	if !strings.Contains(out[FileDockerfile], "FROM ") {
		return errors.New("Dockerfile must contain a FROM instruction")
	}
	ci := out[FilePipeline]
	if !strings.Contains(ci, "artifacts:") || !strings.Contains(ci, ".justcd-build/image-digest") {
		return errors.New(".gitlab-ci.yml must keep the build job that publishes the image digest as the artifact .justcd-build/image-digest")
	}
	if err := validateManifest(c, out[FileDeployment]); err != nil {
		return fmt.Errorf("%s: %w", FileDeployment, err)
	}
	return nil
}

func validateManifest(c Config, text string) error {
	docs, err := parseDocuments(text)
	if err != nil {
		return err
	}
	if len(docs) == 0 || len(docs) > maxResources {
		return errors.New("a manifest must contain between 1 and 20 resources")
	}
	seen := map[string]bool{}
	deployments, services := 0, 0
	for _, r := range docs {
		key := r.kind + "/" + r.name
		if seen[key] {
			return fmt.Errorf("duplicate %s", key)
		}
		seen[key] = true
		meta, _ := r.body["metadata"].(map[string]any)
		if ns, _ := meta["namespace"].(string); ns != c.Namespace {
			return fmt.Errorf("%s: metadata.namespace must be %s", key, c.Namespace)
		}
		if want, ok := generatedKinds[r.kind]; ok {
			if r.body["apiVersion"] != want {
				return fmt.Errorf("%s: apiVersion must be %s", key, want)
			}
		} else if want, ok := extraKinds[r.kind]; !ok {
			return fmt.Errorf("kind %s is not allowed; supported: %s", r.kind, strings.Join(append(sortedKeys(generatedKinds), sortedKeys(extraKinds)...), ", "))
		} else if r.body["apiVersion"] != want {
			return fmt.Errorf("%s: apiVersion must be %s", key, want)
		}
		switch r.kind {
		case "Deployment":
			deployments++
			if r.name != c.Name {
				return fmt.Errorf("the Deployment must be named %s", c.Name)
			}
			if dig(r.body, "spec", "selector", "matchLabels", "app") != c.Name || dig(r.body, "spec", "template", "metadata", "labels", "app") != c.Name {
				return errors.New("the Deployment selector and pod label must be app=" + c.Name)
			}
		case "Service":
			services++
			if r.name != c.Name {
				return fmt.Errorf("the Service must be named %s", c.Name)
			}
		}
		if err := validateResource(c, r); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	if deployments != 1 || services != 1 {
		return errors.New("a manifest needs exactly one Deployment and one Service for the application")
	}
	return nil
}

func validateResource(c Config, r resource) error {
	switch r.kind {
	case "Deployment":
		return validatePod(c, dig(r.body, "spec", "template", "spec"))
	case "Service":
		if t, ok := dig(r.body, "spec", "type").(string); ok && t != "ClusterIP" {
			return errors.New("only ClusterIP services are allowed")
		}
		for _, k := range []string{"externalIPs", "externalName", "loadBalancerIP", "loadBalancerSourceRanges"} {
			if dig(r.body, "spec", k) != nil {
				return errors.New("spec." + k + " is not allowed")
			}
		}
		if dig(r.body, "spec", "selector", "app") != c.Name {
			return errors.New("the Service selector must be app=" + c.Name)
		}
	case "Route":
		if c.Host == "" || dig(r.body, "spec", "host") != c.Host {
			return errors.New("a Route needs the application domain as spec.host")
		}
		if dig(r.body, "spec", "to", "name") != c.Name || dig(r.body, "spec", "to", "kind") != "Service" || dig(r.body, "spec", "alternateBackends") != nil {
			return errors.New("a Route must target the application Service only")
		}
		for _, k := range []string{"key", "certificate", "caCertificate", "destinationCACertificate"} {
			if dig(r.body, "spec", "tls", k) != nil {
				return errors.New("certificate material must not be stored in Git (spec.tls." + k + ")")
			}
		}
	case "Ingress":
		rules, _ := dig(r.body, "spec", "rules").([]any)
		if c.Host == "" || len(rules) == 0 {
			return errors.New("an Ingress needs the application domain and at least one rule")
		}
		for _, rule := range rules {
			if dig(rule, "host") != c.Host {
				return errors.New("rules must use the application host " + c.Host)
			}
			paths, _ := dig(rule, "http", "paths").([]any)
			for _, p := range paths {
				if dig(p, "backend", "service", "name") != c.Name {
					return errors.New("Ingress paths must route to service " + c.Name)
				}
			}
		}
		if dig(r.body, "spec", "defaultBackend") != nil {
			return errors.New("spec.defaultBackend is not allowed")
		}
	case "HorizontalPodAutoscaler":
		if dig(r.body, "spec", "scaleTargetRef", "kind") != "Deployment" || dig(r.body, "spec", "scaleTargetRef", "name") != c.Name {
			return errors.New("scaleTargetRef must be Deployment " + c.Name)
		}
	}
	return nil
}

func parseDocuments(text string) ([]resource, error) {
	var out []resource
	for _, part := range documentSplit.Split(text, -1) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		var body map[string]any
		if err := yaml.UnmarshalStrict([]byte(part), &body); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		if body == nil {
			continue
		}
		kind, _ := body["kind"].(string)
		meta, _ := body["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		if kind == "" || name == "" || body["apiVersion"] == nil {
			return nil, errors.New("every document needs apiVersion, kind and metadata.name")
		}
		if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`).MatchString(name) {
			return nil, fmt.Errorf("%s: name %q is not a valid Kubernetes name", kind, name)
		}
		for _, forbidden := range []string{"ownerReferences", "finalizers", "generateName", "managedFields"} {
			if _, ok := meta[forbidden]; ok {
				return nil, fmt.Errorf("%s %s: metadata.%s is not allowed", kind, name, forbidden)
			}
		}
		out = append(out, resource{kind: kind, name: name, body: body})
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func validatePod(c Config, podAny any) error {
	pod, ok := podAny.(map[string]any)
	if !ok {
		return errors.New("spec.template.spec is required")
	}
	for _, k := range []string{"hostNetwork", "hostPID", "hostIPC", "shareProcessNamespace"} {
		if pod[k] == true {
			return errors.New(k + " is not allowed")
		}
	}
	if pod["automountServiceAccountToken"] == true {
		return errors.New("automountServiceAccountToken must stay false")
	}
	for _, k := range []string{"serviceAccountName", "serviceAccount", "nodeName"} {
		if pod[k] != nil {
			return errors.New("spec.template.spec." + k + " is not allowed")
		}
	}
	if pc, _ := pod["priorityClassName"].(string); strings.HasPrefix(pc, "system-") {
		return errors.New("system priority classes are not allowed")
	}
	if sc, ok := pod["securityContext"].(map[string]any); ok {
		if sc["sysctls"] != nil {
			return errors.New("pod sysctls are not allowed")
		}
		if err := validateSecurityContext(sc); err != nil {
			return err
		}
	}
	volumes, _ := pod["volumes"].([]any)
	for _, v := range volumes {
		m, _ := v.(map[string]any)
		for k := range m {
			if !allowedVolumes[k] {
				return fmt.Errorf("volume type %q is not allowed (allowed: emptyDir, configMap, secret, projected, downwardAPI, persistentVolumeClaim)", k)
			}
		}
	}
	host := registryHost(c.RegistryImage)
	foundApp := false
	for _, group := range []string{"initContainers", "containers"} {
		list, _ := pod[group].([]any)
		for _, item := range list {
			ctr, _ := item.(map[string]any)
			name, _ := ctr["name"].(string)
			image, _ := ctr["image"].(string)
			if group == "containers" && name == appContainer {
				foundApp = true
				if image != imagePlaceholer {
					return errors.New("the image of container app is managed by JustCD and cannot be changed")
				}
			} else if image == "" || strings.Contains(image, imagePlaceholer) || !strings.HasPrefix(image, host+"/") || !hasTagOrDigest(image) {
				return fmt.Errorf("container %s: image must come from registry %s and include a tag or digest", name, host)
			}
			if sc, ok := ctr["securityContext"].(map[string]any); ok {
				if err := validateSecurityContext(sc); err != nil {
					return fmt.Errorf("container %s: %w", name, err)
				}
			}
			ports, _ := ctr["ports"].([]any)
			for _, p := range ports {
				if m, ok := p.(map[string]any); ok && (m["hostPort"] != nil || m["hostIP"] != nil) {
					return fmt.Errorf("container %s: hostPort is not allowed", name)
				}
			}
		}
	}
	if !foundApp {
		return errors.New("container app cannot be removed")
	}
	return nil
}

func validateSecurityContext(sc map[string]any) error {
	if sc["privileged"] == true || sc["allowPrivilegeEscalation"] == true {
		return errors.New("privileged containers and privilege escalation are not allowed")
	}
	if sc["runAsNonRoot"] == false {
		return errors.New("runAsNonRoot cannot be false")
	}
	for _, k := range []string{"runAsUser", "runAsGroup"} {
		if v, ok := sc[k].(float64); ok && v == 0 {
			return errors.New(k + " 0 (root) is not allowed")
		}
	}
	if add, _ := dig(sc, "capabilities", "add").([]any); len(add) > 0 {
		return errors.New("adding Linux capabilities is not allowed")
	}
	if dig(sc, "seccompProfile", "type") == "Unconfined" || sc["procMount"] == "Unmasked" {
		return errors.New("unconfined seccomp and unmasked /proc are not allowed")
	}
	if sc["seLinuxOptions"] != nil {
		return errors.New("seLinuxOptions are not allowed")
	}
	return nil
}

func registryHost(image string) string {
	host, _, _ := strings.Cut(image, "/")
	return host
}

func hasTagOrDigest(image string) bool {
	last := path.Base(image)
	return strings.Contains(last, ":") || strings.Contains(image, "@sha256:")
}
