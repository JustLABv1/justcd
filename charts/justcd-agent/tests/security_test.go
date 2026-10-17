package agentchart

import (
	"encoding/json"
	"fmt"
	"github.com/google/cel-go/cel"
	"os/exec"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"
)

func TestWorkloadAdmissionPolicy(t *testing.T) {
	data, err := exec.Command("helm", "template", "audit", "..", "--kube-version", "1.30.0", "--namespace", "justcd-agent", "--set", "serverUrl=https://justcd.invalid", "--set", "enrollmentSecret=synthetic", "--set", "profiles[0].name=default", "--set", "profiles[0].workspaceIds[0]=workspace", "--set", "profiles[0].namespaces[0]=dev", "--set", "profiles[0].namespaces[1]=preview-*").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	var spec map[string]any
	for _, doc := range strings.Split(string(data), "\n---") {
		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) == nil && obj["kind"] == "ValidatingAdmissionPolicy" {
			spec = obj["spec"].(map[string]any)
		}
	}
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("namespaceObject", cel.DynType), cel.Variable("request", cel.DynType))
	must(err)
	programs := []cel.Program{}
	for _, v := range spec["validations"].([]any) {
		expr := v.(map[string]any)["expression"].(string)
		ast, issues := env.Compile(expr)
		if issues.Err() != nil {
			panic(issues.Err())
		}
		p, e := env.Program(ast)
		must(e)
		programs = append(programs, p)
	}
	ns := map[string]any{"metadata": map[string]any{"labels": map[string]any{"pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"}}}
	cases := []struct {
		name, body string
		want       bool
	}{
		{"safe default", `{"spec":{"containers":[{"name":"web"}]}}`, true},
		{"explicit default", `{"spec":{"serviceAccountName":"default","containers":[{"name":"web"}]}}`, true},
		{"privileged service account", `{"spec":{"serviceAccountName":"cluster-admin","containers":[{"name":"web"}]}}`, false},
		{"secret volume", `{"spec":{"containers":[{}],"volumes":[{"secret":{"secretName":"agent-token"}}]}}`, false},
		{"projected secret", `{"spec":{"containers":[{}],"volumes":[{"projected":{"sources":[{"secret":{"name":"agent-token"}}]}}]}}`, false},
		{"secret environment", `{"spec":{"containers":[{"env":[{"valueFrom":{"secretKeyRef":{"name":"agent-token","key":"token"}}}]}]}}`, false},
		{"init secret", `{"spec":{"containers":[{}],"initContainers":[{"envFrom":[{"secretRef":{"name":"agent-token"}}]}]}}`, false},
	}
	for _, tc := range cases {
		var obj map[string]any
		must(json.Unmarshal([]byte(tc.body), &obj))
		allowed := true
		for _, p := range programs {
			out, _, e := p.Eval(map[string]any{"object": obj, "namespaceObject": ns})
			if e != nil || out.Value() != true {
				allowed = false
			}
		}
		if allowed != tc.want {
			panic(tc.name)
		}
		t.Log("PASS", tc.name)
	}
	for _, labels := range []map[string]any{{}, {"pod-security.kubernetes.io/enforce": "privileged", "pod-security.kubernetes.io/enforce-version": "latest"}} {
		out, _, e := programs[0].Eval(map[string]any{"namespaceObject": map[string]any{"metadata": map[string]any{"labels": labels}}})
		if e == nil && out.Value() == true {
			panic("unsafe namespace accepted")
		}
	}
	t.Log("PASS missing/unsafe namespace labels")
	for _, v := range spec["matchConditions"].([]any) {
		a, i := env.Compile(v.(map[string]any)["expression"].(string))
		must(i.Err())
		p, e := env.Program(a)
		must(e)
		for _, n := range []string{"dev", "preview-12", "prod", "justcd-agent"} {
			out, _, e := p.Eval(map[string]any{"request": map[string]any{"namespace": n}})
			must(e)
			want := n == "dev" || n == "preview-12"
			if out.Value() != want {
				panic("namespace match " + n)
			}
		}
	}
	t.Log("PASS namespace matching")
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}

func TestChartRBACAndNamespaceGuards(t *testing.T) {
	args := []string{"template", "audit", "..", "--kube-version", "1.30.0", "--namespace", "justcd-agent", "--set", "serverUrl=https://justcd.invalid", "--set", "enrollmentSecret=synthetic", "--set", "profiles[0].name=default", "--set", "profiles[0].workspaceIds[0]=workspace", "--set", "profiles[0].namespaces[0]=dev", "--set", "rbac.clusterWide=true"}
	data, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	for _, doc := range strings.Split(string(data), "\n---") {
		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) != nil {
			continue
		}
		switch obj["kind"] {
		case "Role", "ClusterRole":
			for _, raw := range obj["rules"].([]any) {
				rule := raw.(map[string]any)
				for _, key := range []string{"apiGroups", "resources"} {
					if values, ok := rule[key].([]any); ok {
						for _, value := range values {
							if value == "*" {
								t.Fatal("wildcard workload RBAC")
							}
						}
					}
				}
			}
		case "ConfigMap":
			raw := obj["data"].(map[string]any)["config.json"].(string)
			var config struct {
				Profiles []struct {
					DeniedNamespaces []string `json:"deniedNamespaces"`
				} `json:"profiles"`
			}
			if err = json.Unmarshal([]byte(raw), &config); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, ns := range config.Profiles[0].DeniedNamespaces {
				found = found || ns == "justcd-agent"
			}
			if !found {
				t.Fatal("installation namespace not protected")
			}
		case "ValidatingAdmissionPolicy":
			spec := obj["spec"].(map[string]any)
			expression := spec["matchConditions"].([]any)[0].(map[string]any)["expression"].(string)
			env, e := cel.NewEnv(cel.Variable("request", cel.DynType))
			must(e)
			ast, issues := env.Compile(expression)
			must(issues.Err())
			program, e := env.Program(ast)
			must(e)
			for _, ns := range []string{"dev", "other", "justcd-agent", "kube-system", "kube-public", "kube-node-lease"} {
				out, _, e := program.Eval(map[string]any{"request": map[string]any{"namespace": ns}})
				must(e)
				want := ns == "dev" || ns == "other"
				if out.Value() != want {
					t.Fatalf("cluster-wide policy namespace: %s", ns)
				}
			}
		}
	}
	args = append(args, "--set", "profiles[0].namespaces[0]=justcd-agent")
	if data, err = exec.Command("helm", args...).CombinedOutput(); err == nil {
		t.Fatalf("installation namespace accepted: %s", data)
	}
}

func TestCustomKubernetesCredentials(t *testing.T) {
	data, err := exec.Command("helm", "template", "audit", "..", "--namespace", "justcd-agent", "--set", "serverUrl=https://justcd.invalid", "--set", "enrollmentSecret=enrollment", "--set", "profiles[0].workspaceIds[0]=workspace", "--set", "profiles[0].name=default", "--set", "kubernetes.clusterId=operator-cluster", "--set", "kubernetes.serverUrl=https://kube.invalid:6443", "--set", "kubernetes.caSecret=custom-ca", "--set", "kubernetes.tokenSecret=custom-token", "--set", "profiles[1].name=explicit", "--set", "profiles[1].workspaceIds[0]=workspace", "--set", "profiles[1].tokenFile=/custom/token").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	var config map[string]any
	var deployment map[string]any
	for _, doc := range strings.Split(string(data), "\n---") {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatal(err)
		}
		if obj["kind"] == "ConfigMap" {
			if err := json.Unmarshal([]byte(obj["data"].(map[string]any)["config.json"].(string)), &config); err != nil {
				t.Fatal(err)
			}
		}
		if obj["kind"] == "ClusterRole" {
			for _, rule := range obj["rules"].([]any) {
				if names, ok := rule.(map[string]any)["resourceNames"]; ok && strings.Contains(fmt.Sprint(names), "kube-system") {
					t.Fatal("configured identity still grants kube-system access")
				}
			}
		}
		if obj["kind"] == "Deployment" {
			deployment = obj
		}
	}
	if config["clusterId"] != "operator-cluster" {
		t.Fatalf("cluster identity config: %+v", config)
	}
	if config["kubernetesServerUrl"] != "https://kube.invalid:6443" {
		t.Fatalf("endpoint config: %+v", config)
	}
	if config["kubernetesCAFile"] != "/etc/justcd-agent/kubernetes-ca/ca.crt" || config["serverCAFile"] != "" {
		t.Fatalf("CA config: %+v", config)
	}
	profiles := config["profiles"].([]any)
	if profiles[0].(map[string]any)["tokenFile"] != "/etc/justcd-agent/kubernetes-token/token" || profiles[1].(map[string]any)["tokenFile"] != "/custom/token" {
		t.Fatalf("token profiles: %+v", profiles)
	}
	spec := deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	secrets := map[string]string{}
	for _, v := range spec["volumes"].([]any) {
		volume := v.(map[string]any)
		if secret, ok := volume["secret"].(map[string]any); ok {
			secrets[volume["name"].(string)] = secret["secretName"].(string)
		}
	}
	if secrets["kubernetes-ca"] != "custom-ca" || secrets["kubernetes-token"] != "custom-token" {
		t.Fatalf("secret mounts: %+v", secrets)
	}
	mounts := spec["containers"].([]any)[0].(map[string]any)["volumeMounts"].([]any)
	found := 0
	for _, m := range mounts {
		mount := m.(map[string]any)
		if mount["name"] == "kubernetes-ca" || mount["name"] == "kubernetes-token" {
			found++
			if mount["readOnly"] != true || mount["subPath"] != nil {
				t.Fatalf("credential mount: %+v", mount)
			}
		}
	}
	if found != 2 {
		t.Fatalf("credential mount count: %d", found)
	}
}

func TestManagedCredentialBootstrap(t *testing.T) {
	args := []string{"template", "audit", "..", "--namespace", "justcd-agent", "--set", "serverUrl=https://justcd.invalid", "--set", "enrollmentSecret=synthetic", "--set", "profiles[0].name=managed", "--set", "profiles[0].workspaceIds[0]=workspace", "--set", "profiles[0].namespaces[0]=dev", "--set", "profiles[0].deploymentCredentials.lifetimeSeconds=600"}
	data, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	issuer, deployment, account := false, false, false
	for _, doc := range strings.Split(string(data), "\n---") {
		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) != nil {
			continue
		}
		metadata, _ := obj["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		switch obj["kind"] {
		case "Namespace":
			t.Fatal("prepared mode must not create namespaces")
		case "ServiceAccount":
			if name == "justcd-deployer" {
				account = true
				if metadata["namespace"] != "dev" || obj["automountServiceAccountToken"] != false {
					t.Fatal("unsafe deployment account")
				}
			}
		case "Role":
			if name == "audit-justcd-agent" {
				t.Fatal("issuer retained static workload access in managed namespace")
			}
			rules := obj["rules"].([]any)
			if strings.HasSuffix(name, "-issuer") {
				issuer = true
				if metadata["namespace"] != "dev" || len(rules) != 2 {
					t.Fatal("unexpected issuer scope")
				}
				for _, raw := range rules {
					rule := raw.(map[string]any)
					names := rule["resourceNames"].([]any)
					if len(names) != 1 || names[0] != "justcd-deployer" {
						t.Fatal("issuer can select arbitrary accounts")
					}
					resource := rule["resources"].([]any)[0]
					verb := rule["verbs"].([]any)[0]
					if (resource != "serviceaccounts/token" || verb != "create") && (resource != "serviceaccounts" || verb != "get") {
						t.Fatal("issuer has extra privilege")
					}
				}
			} else if strings.HasSuffix(name, "-deployment") {
				deployment = true
				for _, raw := range rules {
					rule := raw.(map[string]any)
					for _, resource := range rule["resources"].([]any) {
						if resource == "secrets" || resource == "serviceaccounts" || resource == "roles" || resource == "rolebindings" || resource == "*" {
							t.Fatal("unsafe default deployment role")
						}
					}
				}
			}
		case "RoleBinding":
			role := obj["roleRef"].(map[string]any)["name"].(string)
			subjects := obj["subjects"].([]any)
			if len(subjects) != 1 {
				t.Fatal("unexpected binding subjects")
			}
			subject := subjects[0].(map[string]any)
			if strings.HasSuffix(role, "-deployment") && (subject["name"] != "justcd-deployer" || subject["namespace"] != "dev") {
				t.Fatal("workload role granted to issuer")
			}
			if strings.HasSuffix(role, "-issuer") && (subject["name"] != "audit-justcd-agent" || subject["namespace"] != "justcd-agent") {
				t.Fatal("wrong issuer binding")
			}
		}
	}
	if !issuer || !deployment || !account {
		t.Fatal("bootstrap resources missing")
	}
	for _, override := range []string{"profiles[0].namespaces[0]=*", "profiles[0].namespaces[0]=preview-*", "profiles[0].namespaces[0]=default", "profiles[0].namespaces[0]=kube-system", "profiles[0].namespaces[0]=justcd-agent", "profiles[0].clusterScope=true", "profiles[0].deploymentCredentials.lifetimeSeconds=599", "rbac.clusterWide=true", "profiles[0].workspaceIds[1]=other", "managedCredentials.rules[0].apiGroups[0]=*", "managedCredentials.rules[0].apiGroups[0]=rbac.authorization.k8s.io", "managedCredentials.rules[0].resources[0]=serviceaccounts/token", "managedCredentials.rules[0].verbs[0]=impersonate"} {
		t.Run(override, func(t *testing.T) {
			if _, err := exec.Command("helm", append(append([]string{}, args...), "--set", override)...).CombinedOutput(); err == nil {
				t.Fatal("unsafe managed profile accepted")
			}
		})
	}
	data, err = exec.Command("helm", append(append([]string{}, args...), "--set", "rbac.create=false")...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	if strings.Contains(string(data), "kind: RoleBinding") || strings.Contains(string(data), "name: justcd-deployer") {
		t.Fatal("external RBAC mode changed prepared resources")
	}
}

func TestNamespaceProvisionerRBACAndAdmission(t *testing.T) {
	args := []string{"template", "audit", "..", "--namespace", "justcd-agent", "--set", "serverUrl=https://justcd.invalid", "--set", "enrollmentSecret=synthetic", "--set", "profiles[0].name=managed", "--set", "profiles[0].workspaceIds[0]=workspace", "--set", "profiles[0].namespaces[0]=preview-*", "--set", "profiles[0].deploymentCredentials.lifetimeSeconds=600", "--set", "profiles[0].deploymentCredentials.provisioning.deploymentClusterRole=approved", "--set", "profiles[0].deploymentCredentials.provisioning.deleteNamespaces=true"}
	// Lint validates raw template separators before helm template sorts documents.
	// Keep both checks: sorting can mask whitespace-trimmed document boundaries.
	lintArgs := append([]string{"lint", ".."}, args[3:]...)
	if data, err := exec.Command("helm", lintArgs...).CombinedOutput(); err != nil {
		t.Fatalf("helm lint: %v: %s", err, data)
	}
	data, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	var policy map[string]any
	roleFound := false
	for _, doc := range strings.Split(string(data), "\n---") {
		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) != nil {
			continue
		}
		metadata, _ := obj["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		if obj["kind"] == "Namespace" {
			t.Fatal("Helm must not own runtime environment namespaces")
		}
		if obj["kind"] == "RoleBinding" || obj["kind"] == "Role" {
			t.Fatal("automatic profile tried to bootstrap a wildcard namespace")
		}
		if obj["kind"] == "ClusterRole" && strings.HasSuffix(name, "-provisioner") {
			roleFound = true
			for _, raw := range obj["rules"].([]any) {
				rule := raw.(map[string]any)
				resource := rule["resources"].([]any)[0].(string)
				for _, verb := range rule["verbs"].([]any) {
					if verb == "*" || verb == "escalate" || verb == "impersonate" || verb == "patch" || verb == "update" {
						t.Fatal("provisioner received unnecessary escalation/write permissions")
					}
				}
				if resource == "secrets" || resource == "roles" || resource == "clusterrolebindings" {
					t.Fatal("provisioner received unrelated privileges")
				}
				if resource == "clusterroles" {
					names := rule["resourceNames"].([]any)
					if len(names) != 1 || names[0] != "approved" || rule["verbs"].([]any)[0] != "bind" {
						t.Fatal("unrestricted role binding privilege")
					}
				}
				if resource == "serviceaccounts/token" {
					names := rule["resourceNames"].([]any)
					if len(names) != 1 || names[0] != "justcd-deployer" {
						t.Fatal("arbitrary account issuance")
					}
				}
			}
		}
		if obj["kind"] == "ValidatingAdmissionPolicy" && strings.HasSuffix(name, "-provisioner") {
			policy = obj["spec"].(map[string]any)
		}
	}
	if !roleFound || policy == nil {
		t.Fatal("provisioner bootstrap is missing")
	}
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("oldObject", cel.DynType), cel.Variable("request", cel.DynType), cel.Variable("variables", cel.DynType))
	must(err)
	programs := []cel.Program{}
	for _, raw := range policy["validations"].([]any) {
		ast, issues := env.Compile(raw.(map[string]any)["expression"].(string))
		must(issues.Err())
		program, e := env.Program(ast)
		must(e)
		programs = append(programs, program)
	}
	cases := []struct {
		name, resource, subresource, target, account, role, subject string
		lifetime                                                    int
		want                                                        bool
	}{
		{name: "namespace", resource: "namespaces", target: "preview-12", want: true},
		{name: "outside prefix", resource: "namespaces", target: "production", want: false},
		{name: "protected namespace", resource: "namespaces", target: "kube-system", want: false},
		{name: "deployment account", resource: "serviceaccounts", target: "preview-12", account: "justcd-deployer", want: true},
		{name: "arbitrary account", resource: "serviceaccounts", target: "preview-12", account: "admin", want: false},
		{name: "token", resource: "serviceaccounts", subresource: "token", target: "preview-12", account: "justcd-deployer", lifetime: 600, want: true},
		{name: "long token", resource: "serviceaccounts", subresource: "token", target: "preview-12", account: "justcd-deployer", lifetime: 3600, want: false},
		{name: "unapproved token", resource: "serviceaccounts", subresource: "token", target: "preview-12", account: "default", lifetime: 600, want: false},
		{name: "cross namespace token", resource: "serviceaccounts", subresource: "token", target: "production", account: "justcd-deployer", lifetime: 600, want: false},
		{name: "approved role", resource: "rolebindings", target: "preview-12", role: "approved", subject: "justcd-deployer", want: true},
		{name: "admin role", resource: "rolebindings", target: "preview-12", role: "cluster-admin", subject: "justcd-deployer", want: false},
		{name: "foreign subject", resource: "rolebindings", target: "preview-12", role: "approved", subject: "admin", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			object := map[string]any{"metadata": map[string]any{"name": tc.account}, "spec": map[string]any{"expirationSeconds": tc.lifetime}}
			if tc.resource == "rolebindings" {
				object["metadata"] = map[string]any{"name": "justcd-deployer"}
				object["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": tc.role}
				object["subjects"] = []any{map[string]any{"kind": "ServiceAccount", "name": tc.subject, "namespace": tc.target}}
			}
			request := map[string]any{"resource": map[string]any{"resource": tc.resource}, "operation": "CREATE", "subResource": tc.subresource, "namespace": tc.target, "name": tc.account}
			allowed := true
			for _, program := range programs {
				out, _, e := program.Eval(map[string]any{"object": object, "oldObject": nil, "request": request, "variables": map[string]any{"target": tc.target}})
				if e != nil || out.Value() != true {
					allowed = false
				}
			}
			if allowed != tc.want {
				t.Fatalf("admission allowed=%t, want %t", allowed, tc.want)
			}
		})
	}
}
