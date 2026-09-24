package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const (
	maxManifestFileBytes = 2 << 20
	maxRenderOutputBytes = 64 << 20
	maxRenderedResources = 10000
)

type Options struct {
	RepositoryRoot string
	ManifestPath   string
	Renderer       string
	ApplicationID  string
	ClusterID      string
	Namespaces     []core.Binding
	Mapper         meta.RESTMapper
}

func Render(ctx context.Context, opts Options) ([]core.Resource, error) {
	if opts.RepositoryRoot == "" || opts.ManifestPath == "" || opts.ApplicationID == "" || opts.ClusterID == "" || opts.Mapper == nil {
		return nil, errors.New("render target and discovery mapper are required")
	}
	manifestPath, err := safeRepositoryPath(opts.RepositoryRoot, opts.ManifestPath)
	if err != nil {
		return nil, err
	}
	var output []byte
	switch opts.Renderer {
	case "yaml":
		output, err = readManifestFiles(manifestPath)
	case "kustomize":
		if err := validateKustomizeSources(manifestPath, opts.RepositoryRoot); err != nil {
			return nil, err
		}
		output, err = runRenderer(ctx, "kustomize", []string{"build", "--load-restrictor", "LoadRestrictionsRootOnly", manifestPath}, opts.RepositoryRoot)
	case "helm":
		if len(opts.Namespaces) != 1 {
			return nil, errors.New("Helm applications require exactly one bound namespace")
		}
		output, err = runRenderer(ctx, "helm", []string{"template", "justcd", manifestPath, "--include-crds", "--namespace", opts.Namespaces[0].Namespace}, opts.RepositoryRoot)
	default:
		return nil, fmt.Errorf("unsupported renderer %q", opts.Renderer)
	}
	if err != nil {
		return nil, err
	}
	return parseAndNormalize(output, opts)
}

func safeRepositoryPath(root, relative string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	requested := filepath.Join(rootAbs, filepath.FromSlash(relative))
	rel, err := filepath.Rel(rootAbs, requested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("manifestPath escapes the repository")
	}
	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("manifestPath cannot be resolved: %w", err)
	}
	resolvedAbs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	resolvedRel, err := filepath.Rel(rootAbs, resolvedAbs)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return "", errors.New("manifestPath resolves outside the repository")
	}
	return resolvedAbs, nil
}

func readManifestFiles(root string) ([]byte, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	files := []string{}
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(root))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil, errors.New("manifest file must end in .yaml, .yml, or .json")
		}
		files = append(files, root)
	} else {
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext == ".yaml" || ext == ".yml" || ext == ".json" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) > maxRenderedResources {
		return nil, errors.New("manifest contains too many files")
	}
	var output bytes.Buffer
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		if info.Size() > maxManifestFileBytes {
			return nil, fmt.Errorf("manifest file %q exceeds 2 MiB", filepath.Base(file))
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if output.Len()+len(content)+4 > maxRenderOutputBytes {
			return nil, errors.New("manifest output exceeds configured size limit")
		}
		output.Write(content)
		output.WriteString("\n---\n")
	}
	return output.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("renderer output exceeds configured size limit")
	}
	return b.Buffer.Write(p)
}

func runRenderer(parent context.Context, name string, args []string, repositoryRoot string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("", "justcd-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repositoryRoot
	cmd.Env = cleanEnvironment(os.Environ(), home)
	stdout := &cappedBuffer{max: maxRenderOutputBytes}
	stderr := &cappedBuffer{max: 1 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s renderer timed out", name)
		}
		return nil, fmt.Errorf("%s renderer failed", name)
	}
	return stdout.Bytes(), nil
}

func cleanEnvironment(in []string, home string) []string {
	allowed := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "SYSTEMROOT": true, "WINDIR": true, "HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	out := make([]string, 0, len(allowed)+12)
	for _, pair := range in {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || !allowed[strings.ToUpper(key)] {
			continue
		}
		out = append(out, pair)
	}
	return append(out, "HOME="+home, "TMPDIR="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "HELM_CONFIG_HOME="+filepath.Join(home, ".config", "helm"), "HELM_CACHE_HOME="+filepath.Join(home, ".cache", "helm"), "HELM_DATA_HOME="+filepath.Join(home, ".local", "share", "helm"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func validateKustomizeSources(root, repositoryRoot string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name != "kustomization.yaml" && name != "kustomization.yml" && name != "kustomization.json" {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(content) > maxManifestFileBytes {
			return errors.New("kustomization file exceeds 2 MiB")
		}
		var data map[string]any
		if err := yaml.Unmarshal(content, &data); err != nil {
			return errors.New("invalid kustomization file")
		}
		for _, field := range []string{"resources", "bases", "components"} {
			if raw, ok := data[field]; ok {
				list, ok := raw.([]any)
				if !ok {
					return fmt.Errorf("kustomize %s must be a list", field)
				}
				for _, item := range list {
					value, ok := item.(string)
					if !ok || !localKustomizePath(value) {
						return errors.New("remote or absolute Kustomize bases/resources are not supported")
					}
				}
			}
		}
		if _, ok := data["helmCharts"]; ok {
			return errors.New("Kustomize Helm chart rendering is not supported; use the Helm renderer")
		}
		rel, err := filepath.Rel(repositoryRoot, p)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("kustomization is outside repository root")
		}
		return nil
	})
}

func localKustomizePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") || strings.HasPrefix(value, "git::") || strings.HasPrefix(value, "github.com/") {
		return false
	}
	clean := filepath.Clean(value)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func parseAndNormalize(output []byte, opts Options) ([]core.Resource, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	allowedNamespaces := map[string]bool{}
	for _, binding := range opts.Namespaces {
		allowedNamespaces[binding.Namespace] = true
	}
	resources := make([]core.Resource, 0)
	for index := 0; ; index++ {
		var object map[string]interface{}
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode rendered document %d: %w", index+1, err)
		}
		if len(object) == 0 {
			continue
		}
		if len(resources) >= maxRenderedResources {
			return nil, errors.New("rendered output exceeds 10000 resources")
		}
		resource := unstructured.Unstructured{Object: object}
		apiVersion := resource.GetAPIVersion()
		kind := resource.GetKind()
		name := resource.GetName()
		if apiVersion == "" || kind == "" || name == "" {
			return nil, fmt.Errorf("rendered document %d needs apiVersion, kind, and metadata.name", index+1)
		}
		groupVersion, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid apiVersion in rendered document %d", index+1)
		}
		mapping, err := opts.Mapper.RESTMapping(schema.GroupKind{Group: groupVersion.Group, Kind: kind}, groupVersion.Version)
		if err != nil {
			return nil, fmt.Errorf("Kubernetes API does not recognize %s %s: %w", apiVersion, kind, err)
		}
		clusterScoped := mapping.Scope.Name() != meta.RESTScopeNameNamespace
		if clusterScoped && resource.GetNamespace() != "" {
			return nil, fmt.Errorf("cluster-scoped resource %s/%s must not declare a namespace", kind, name)
		}
		if !clusterScoped {
			namespace := resource.GetNamespace()
			if namespace == "" {
				if len(allowedNamespaces) != 1 {
					return nil, fmt.Errorf("resource %s/%s must declare a namespace when the application has multiple namespace bindings", kind, name)
				}
				for only := range allowedNamespaces {
					namespace = only
				}
				resource.SetNamespace(namespace)
			}
			if !allowedNamespaces[namespace] {
				return nil, fmt.Errorf("resource %s/%s targets unbound namespace %q", kind, name, namespace)
			}
		}
		metadata, found, err := unstructured.NestedMap(resource.Object, "metadata")
		if err != nil || !found {
			return nil, fmt.Errorf("rendered document %d has invalid metadata", index+1)
		}
		stripServerMetadata(metadata)
		if err := unstructured.SetNestedMap(resource.Object, metadata, "metadata"); err != nil {
			return nil, err
		}
		delete(resource.Object, "status")
		labels := resource.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["justcd.io/application-id"] = opts.ApplicationID
		resource.SetLabels(labels)
		canonical, err := json.Marshal(resource.Object)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(canonical)
		resources = append(resources, core.Resource{Identity: core.Identity{ClusterID: opts.ClusterID, APIVersion: apiVersion, Kind: kind, Namespace: resource.GetNamespace(), Name: name, ClusterScoped: clusterScoped}, Fingerprint: hex.EncodeToString(sum[:]), Manifest: canonical})
	}
	return resources, nil
}

func CanonicalLive(object *unstructured.Unstructured, clusterID, applicationID string, clusterScoped bool) (core.Resource, error) {
	copy := object.DeepCopy()
	metadata, found, err := unstructured.NestedMap(copy.Object, "metadata")
	if err != nil || !found {
		return core.Resource{}, errors.New("live object has invalid metadata")
	}
	stripServerMetadata(metadata)
	if err := unstructured.SetNestedMap(copy.Object, metadata, "metadata"); err != nil {
		return core.Resource{}, err
	}
	delete(copy.Object, "status")
	canonical, err := json.Marshal(copy.Object)
	if err != nil {
		return core.Resource{}, err
	}
	sum := sha256.Sum256(canonical)
	identity := core.Identity{ClusterID: clusterID, APIVersion: copy.GetAPIVersion(), Kind: copy.GetKind(), Namespace: copy.GetNamespace(), Name: copy.GetName(), ClusterScoped: clusterScoped}
	return core.Resource{Identity: identity, Fingerprint: hex.EncodeToString(sum[:]), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), Owner: object.GetLabels()["justcd.io/application-id"], Manifest: canonical}, nil
}

// CanonicalLiveAgainst compares only fields declared in the current or last
// applied manifest. Kubernetes defaults and fields owned by other controllers
// therefore do not create permanent drift, while fields removed from Git stay
// in the comparison set until JustCD has pruned them.
func CanonicalLiveAgainst(object *unstructured.Unstructured, clusterID, applicationID string, clusterScoped bool, desiredManifest, previousManifest []byte) (core.Resource, string, error) {
	live := object.DeepCopy()
	metadata, found, err := unstructured.NestedMap(live.Object, "metadata")
	if err != nil || !found {
		return core.Resource{}, "", errors.New("live object has invalid metadata")
	}
	stripServerMetadata(metadata)
	if err := unstructured.SetNestedMap(live.Object, metadata, "metadata"); err != nil {
		return core.Resource{}, "", err
	}
	delete(live.Object, "status")
	var desired map[string]interface{}
	if err := json.Unmarshal(desiredManifest, &desired); err != nil {
		return core.Resource{}, "", fmt.Errorf("decode desired manifest for comparison: %w", err)
	}
	var previous map[string]interface{}
	if len(previousManifest) > 0 {
		if err := json.Unmarshal(previousManifest, &previous); err != nil {
			return core.Resource{}, "", fmt.Errorf("decode stored manifest for comparison: %w", err)
		}
	}
	paths := make([][]string, 0, 64)
	collectManagedPaths(desired, nil, &paths)
	collectManagedPaths(previous, nil, &paths)
	seen := map[string]bool{}
	unique := make([][]string, 0, len(paths))
	for _, path := range paths {
		key := strings.Join(path, "\x00")
		if !seen[key] {
			seen[key] = true
			unique = append(unique, path)
		}
	}
	projectedDesired := projectFields(desired, unique)
	projectedLive := projectFields(live.Object, unique)
	desiredJSON, err := json.Marshal(projectedDesired)
	if err != nil {
		return core.Resource{}, "", err
	}
	liveProjectionJSON, err := json.Marshal(projectedLive)
	if err != nil {
		return core.Resource{}, "", err
	}
	canonical, err := json.Marshal(live.Object)
	if err != nil {
		return core.Resource{}, "", err
	}
	identity := core.Identity{ClusterID: clusterID, APIVersion: live.GetAPIVersion(), Kind: live.GetKind(), Namespace: live.GetNamespace(), Name: live.GetName(), ClusterScoped: clusterScoped}
	resource := core.Resource{Identity: identity, Fingerprint: fingerprint(liveProjectionJSON), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), Owner: object.GetLabels()["justcd.io/application-id"], Manifest: canonical}
	return resource, fingerprint(desiredJSON), nil
}

func stripServerMetadata(metadata map[string]interface{}) {
	for _, field := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink"} {
		delete(metadata, field)
	}
}
func fingerprint(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func collectManagedPaths(value interface{}, prefix []string, out *[][]string) {
	object, ok := value.(map[string]interface{})
	if !ok {
		if len(prefix) > 0 {
			*out = append(*out, append([]string(nil), prefix...))
		}
		return
	}
	if len(object) == 0 {
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		if len(prefix) == 0 && key == "status" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		collectManagedPaths(object[key], append(prefix, key), out)
	}
}
func projectFields(source map[string]interface{}, paths [][]string) map[string]interface{} {
	projected := map[string]interface{}{}
	for _, fieldPath := range paths {
		value, ok := lookupPath(source, fieldPath)
		if ok {
			setPath(projected, fieldPath, value)
		}
	}
	return projected
}
func lookupPath(source map[string]interface{}, fieldPath []string) (interface{}, bool) {
	var current interface{} = source
	for _, part := range fieldPath {
		object, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
func setPath(target map[string]interface{}, fieldPath []string, value interface{}) {
	current := target
	for _, part := range fieldPath[:len(fieldPath)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	current[fieldPath[len(fieldPath)-1]] = value
}

func NormalizedObject(resource core.Resource) (*unstructured.Unstructured, error) {
	var object map[string]interface{}
	if err := json.Unmarshal(resource.Manifest, &object); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: object}, nil
}
