package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/store"
	"sigs.k8s.io/yaml"
)

// A nil scope means dependency discovery was incomplete: never skip that PR.
func (s *Server) kustomizeReviewPaths(ctx context.Context, source store.GitSource, app store.Application, connection store.SourceControlConnection, review store.PullRequestReview) []string {
	checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, pullRequestRef(connection.Provider, review.Number))
	if err != nil {
		return nil
	}
	defer checkout.Close()
	if checkout.Commit != review.HeadSHA {
		return nil
	}
	paths, err := kustomizeDependencies(checkout.Root, configuredReviewPaths(app, connection.PreviewProfile))
	if err != nil {
		return nil
	}
	return paths
}

// Watch each referenced directory, but recurse only through its selected
// kustomization. Unrelated sibling applications never become dependencies.
func kustomizeDependencies(root string, inputs []string) ([]string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	watched := map[string]bool{}
	var visit func(string, string, bool) error
	visit = func(base, input string, recurse bool) error {
		if input == "" {
			return nil
		}
		if strings.Contains(input, ":") || strings.Contains(input, "?") || filepath.IsAbs(input) {
			return errors.New("nonlocal dependency")
		}
		candidate := filepath.Clean(filepath.Join(base, input))
		rel, err := filepath.Rel(root, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return errors.New("dependency escapes repository")
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return err
		}
		resolvedRel, err := filepath.Rel(root, resolved)
		if err != nil || resolvedRel != rel {
			return errors.New("symlink dependency")
		}
		watched[filepath.ToSlash(rel)] = true
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() || !recurse {
			return err
		}
		if seen[candidate] {
			return nil
		}
		if len(seen) >= 1000 {
			return errors.New("too many dependencies")
		}
		seen[candidate] = true
		var content []byte
		for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization", "kustomization.json"} {
			p := filepath.Join(candidate, name)
			info, statErr := os.Lstat(p)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr != nil {
				return statErr
			}
			if !info.Mode().IsRegular() || info.Size() > 2<<20 {
				return errors.New("invalid kustomization file")
			}
			content, err = os.ReadFile(p)
			if err != nil {
				return err
			}
			break
		}
		if content == nil {
			return errors.New("missing kustomization")
		}
		var data map[string]any
		if err := yaml.UnmarshalStrict(content, &data); err != nil {
			return err
		}
		// Unknown features may read additional files. Fail open for PR inclusion.
		inline := strings.Fields("apiVersion kind metadata namePrefix nameSuffix namespace commonLabels commonAnnotations labels annotations images replicas vars generatorOptions buildMetadata sortOptions")
		supported := strings.Fields("resources bases components crds configurations patchesStrategicMerge patches patchesJson6902 replacements configMapGenerator secretGenerator openapi")
		allowed := map[string]bool{}
		for _, key := range append(inline, supported...) {
			allowed[key] = true
		}
		for key := range data {
			if !allowed[key] {
				return errors.New("unsupported dependency feature")
			}
		}
		for _, key := range strings.Fields("resources bases components crds configurations patchesStrategicMerge") {
			if data[key] == nil {
				continue
			}
			entries, ok := data[key].([]any)
			if !ok {
				return errors.New("invalid dependency list")
			}
			for _, entry := range entries {
				value, ok := entry.(string)
				if !ok {
					return errors.New("invalid dependency")
				}
				// Inline strategic merge patches need no external input.
				if key == "patchesStrategicMerge" && strings.Contains(value, "\n") {
					continue
				}
				if err := visit(candidate, value, key == "resources" || key == "bases" || key == "components"); err != nil {
					return err
				}
			}
		}
		for _, key := range strings.Fields("patches patchesJson6902 replacements configMapGenerator secretGenerator") {
			if data[key] == nil {
				continue
			}
			entries, ok := data[key].([]any)
			if !ok {
				return errors.New("invalid dependency list")
			}
			for _, raw := range entries {
				entry, ok := raw.(map[string]any)
				if !ok {
					return errors.New("invalid dependency entry")
				}
				for _, field := range []string{"path", "env"} {
					if value, ok := entry[field].(string); ok {
						if err := visit(candidate, value, false); err != nil {
							return err
						}
					}
				}
				for _, field := range []string{"files", "envs"} {
					if entry[field] == nil {
						continue
					}
					values, ok := entry[field].([]any)
					if !ok {
						return errors.New("invalid generator sources")
					}
					for _, raw := range values {
						value, ok := raw.(string)
						if !ok {
							return errors.New("invalid generator source")
						}
						if field == "files" {
							if _, filename, named := strings.Cut(value, "="); named {
								value = filename
							}
						}
						if err := visit(candidate, value, false); err != nil {
							return err
						}
					}
				}
			}
		}
		if openapi, ok := data["openapi"].(map[string]any); ok {
			if value, ok := openapi["path"].(string); ok {
				if err := visit(candidate, value, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, input := range inputs {
		if err := visit(root, input, true); err != nil {
			return nil, err
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no overlays")
	}
	paths := make([]string, 0, len(watched))
	for p := range watched {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}
