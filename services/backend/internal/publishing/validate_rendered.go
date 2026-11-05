package publishing

import (
	"encoding/json"
	"errors"
	"fmt"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"net"
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
	extraKinds     = map[string]string{"SGCluster": "stackgres.io/v1", "SGScript": "stackgres.io/v1", "VaultStaticSecret": "secrets.hashicorp.com/v1beta1", "ConfigMap": "v1", "NetworkPolicy": "networking.k8s.io/v1", "PodDisruptionBudget": "policy/v1", "HorizontalPodAutoscaler": "autoscaling/v2"}
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
	case "SGCluster", "SGScript", "VaultStaticSecret":
		return validateServiceObject(c, r)
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
	case "NetworkPolicy":
		return validateNetworkPolicy(c, r)
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

// Policies may affect only this application's pods, never every pod in a shared namespace.
func validateNetworkPolicy(c Config, r resource) error {
	b, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	var policy networkingv1.NetworkPolicy
	if err := yaml.UnmarshalStrict(b, &policy); err != nil {
		return fmt.Errorf("invalid NetworkPolicy: %w", err)
	}
	selector := policy.Spec.PodSelector
	selectedApp := selector.MatchLabels["app"] == c.Name
	if services, err := resolvedServices(c); err == nil && services.Database != nil && selector.MatchLabels["app"] == ServiceResourceName(c.Name, "-db") {
		selectedApp = true
	}
	if len(selector.MatchLabels) != 1 || !selectedApp || len(selector.MatchExpressions) != 0 {
		return errors.New("NetworkPolicy must select exactly this application's app pods or its selected database pods using matchLabels.app")
	}
	seen := map[networkingv1.PolicyType]bool{}
	for _, typ := range policy.Spec.PolicyTypes {
		if (typ != networkingv1.PolicyTypeIngress && typ != networkingv1.PolicyTypeEgress) || seen[typ] {
			return errors.New("NetworkPolicy policyTypes must contain unique Ingress/Egress values")
		}
		seen[typ] = true
	}
	checkPeers := func(peers []networkingv1.NetworkPolicyPeer) error {
		for _, peer := range peers {
			for _, sel := range []*metav1.LabelSelector{peer.NamespaceSelector, peer.PodSelector} {
				if sel != nil {
					if _, err := metav1.LabelSelectorAsSelector(sel); err != nil {
						return fmt.Errorf("invalid peer selector: %w", err)
					}
				}
			}
			if peer.IPBlock != nil {
				if peer.NamespaceSelector != nil || peer.PodSelector != nil {
					return errors.New("ipBlock cannot be combined with selectors in one peer")
				}
				_, block, err := net.ParseCIDR(peer.IPBlock.CIDR)
				if err != nil {
					return errors.New("invalid NetworkPolicy ipBlock CIDR")
				}
				for _, except := range peer.IPBlock.Except {
					ip, subnet, err := net.ParseCIDR(except)
					if err != nil {
						return errors.New("invalid NetworkPolicy ipBlock exception")
					}
					bits, _ := block.Mask.Size()
					subBits, _ := subnet.Mask.Size()
					if !block.Contains(ip) || subBits <= bits {
						return errors.New("ipBlock exceptions must be strict subnets of the CIDR")
					}
				}
			}
		}
		return nil
	}
	checkPorts := func(ports []networkingv1.NetworkPolicyPort) error {
		for _, p := range ports {
			if p.Protocol != nil && *p.Protocol != "TCP" && *p.Protocol != "UDP" && *p.Protocol != "SCTP" {
				return errors.New("invalid NetworkPolicy port protocol")
			}
			if p.Port != nil {
				if p.Port.Type == intstr.Int {
					if p.Port.IntVal < 1 || p.Port.IntVal > 65535 {
						return errors.New("NetworkPolicy port must be between 1 and 65535")
					}
				} else if len(validation.IsValidPortName(p.Port.StrVal)) != 0 {
					return errors.New("invalid NetworkPolicy named port")
				}
			}
			if p.EndPort != nil && (p.Port == nil || p.Port.Type != intstr.Int || *p.EndPort < p.Port.IntVal || *p.EndPort > 65535) {
				return errors.New("invalid NetworkPolicy endPort")
			}
		}
		return nil
	}
	for _, rule := range policy.Spec.Ingress {
		if err := checkPeers(rule.From); err != nil {
			return err
		}
		if err := checkPorts(rule.Ports); err != nil {
			return err
		}
	}
	for _, rule := range policy.Spec.Egress {
		if err := checkPeers(rule.To); err != nil {
			return err
		}
		if err := checkPorts(rule.Ports); err != nil {
			return err
		}
	}
	return nil
}
