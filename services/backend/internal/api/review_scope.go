package api

import (
	"path"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// reviewPaths lists repository inputs that JustCD renders for this application.
// Kustomize can import bases outside its overlay; the configured path alone is
// insufficient alone; the worker discovers their dependencies from Git.
func reviewPaths(app store.Application, profile store.PreviewProfile) []string {
	if app.Renderer == "kustomize" {
		return nil
	}
	return configuredReviewPaths(app, profile)
}

func configuredReviewPaths(app store.Application, profile store.PreviewProfile) []string {
	paths := []string{app.ManifestPath, app.TargetManifestPath}
	for _, p := range app.NamespaceManifestPaths {
		paths = append(paths, p)
	}
	paths = append(paths, app.HelmValuesFiles...)
	paths = append(paths, app.TargetHelmValuesFiles...)
	for _, values := range app.NamespaceHelmValues {
		paths = append(paths, values.Files...)
	}
	if profile.Enabled {
		paths = append(paths, profile.ManifestPath)
		paths = append(paths, profile.HelmValuesFiles...)
	}
	return paths
}

func pathsAffectApplication(changed, watched []string) bool {
	if watched == nil {
		return true
	}
	for _, file := range changed {
		file = path.Clean(file)
		for _, watchedPath := range watched {
			if watchedPath == "" {
				continue
			}
			watchedPath = path.Clean(watchedPath)
			if watchedPath == "." || file == watchedPath || strings.HasPrefix(file, watchedPath+"/") {
				return true
			}
		}
	}
	return false
}
