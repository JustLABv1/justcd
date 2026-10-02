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
