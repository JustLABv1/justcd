package api

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type applicationGroupNamespaceInput struct {
	Namespace string   `json:"namespace"`
	Files     []string `json:"valuesFiles"`
	YAML      string   `json:"valuesYaml"`
}

type applicationGroupTargetInput struct {
	Name                   string                           `json:"name"`
	ClusterID              string                           `json:"clusterId"`
	TargetManifestPath     string                           `json:"targetManifestPath"`
	NamespaceManifestPaths map[string]string                `json:"namespaceManifestPaths"`
	Namespaces             []applicationGroupNamespaceInput `json:"namespaces"`
	TargetHelmValuesFiles  []string                         `json:"targetHelmValuesFiles"`
	TargetHelmValuesYAML   string                           `json:"targetHelmValuesYaml"`
}

type applicationGroupInput struct {
	WorkspaceID                string                        `json:"workspaceId"`
	Name                       string                        `json:"name"`
	SourceID                   string                        `json:"sourceId"`
	Revision                   string                        `json:"revision"`
	ManifestPath               string                        `json:"manifestPath"`
	Renderer                   string                        `json:"renderer"`
	KustomizeHelmEnabled       bool                          `json:"kustomizeHelmEnabled"`
	KustomizeNamespaceOverride bool                          `json:"kustomizeNamespaceOverride"`
	HelmValuesFiles            []string                      `json:"helmValuesFiles"`
	HelmValuesYAML             string                        `json:"helmValuesYaml"`
	SyncPolicy                 string                        `json:"syncPolicy"`
	PollSeconds                int                           `json:"pollSeconds"`
	Targets                    []applicationGroupTargetInput `json:"targets"`
}

func cleanGroupSource(input *applicationGroupInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Revision = strings.TrimSpace(input.Revision)
	if !resourceNamePattern.MatchString(input.Name) || input.Revision == "" || len(input.Revision) > 256 || strings.HasPrefix(input.Revision, "-") || strings.ContainsAny(input.Revision, " \t\r\n\x00") {
		return errors.New("invalid deployment group name or Git revision")
	}
	input.ManifestPath = path.Clean(strings.TrimSpace(input.ManifestPath))
	if input.ManifestPath == "." || input.ManifestPath == ".." || strings.HasPrefix(input.ManifestPath, "../") || path.IsAbs(input.ManifestPath) || strings.ContainsRune(input.ManifestPath, '\x00') {
		return errors.New("manifest path must be repository-relative")
	}
	if input.Renderer == "" {
		input.Renderer = "helm"
	}
	if input.Renderer != "helm" && input.Renderer != "kustomize" {
		return errors.New("deployment groups support Helm or Kustomize")
	}
	if (input.KustomizeHelmEnabled || input.KustomizeNamespaceOverride) && input.Renderer != "kustomize" {
		return errors.New("Kustomize settings require the Kustomize renderer")
	}
	files, values, err := validateHelmValuesInput(input.Renderer, input.HelmValuesFiles, input.HelmValuesYAML)
	if err != nil {
		return err
	}
	input.HelmValuesFiles, input.HelmValuesYAML = files, values
	if input.SyncPolicy == "" {
		input.SyncPolicy = "manual"
	}
	if input.SyncPolicy != "manual" && input.SyncPolicy != "auto-safe" {
		return errors.New("syncPolicy must be manual or auto-safe")
	}
	if input.PollSeconds == 0 {
		input.PollSeconds = 300
	}
	if input.PollSeconds < 30 || input.PollSeconds > 86400 {
		return errors.New("pollSeconds must be between 30 and 86400")
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 {
		return errors.New("add between 1 and 100 targets")
	}
	return nil
}

func (s *Server) createApplicationGroup(w http.ResponseWriter, r *http.Request) {
	var input applicationGroupInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
		return
	}
	if err := cleanGroupSource(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	canUseSource, sourceErr := s.Store.WorkspaceCanUseGitSource(r.Context(), input.WorkspaceID, input.SourceID)
	if sourceErr != nil || !canUseSource {
		writeError(w, http.StatusBadRequest, "Git source is not available to this workspace")
		return
	}
	group := store.ApplicationGroup{ID: store.NewID(), WorkspaceID: input.WorkspaceID, Name: input.Name, SourceID: input.SourceID, Revision: input.Revision, ManifestPath: input.ManifestPath, Renderer: input.Renderer, KustomizeHelmEnabled: input.KustomizeHelmEnabled, KustomizeNamespaceOverride: input.KustomizeNamespaceOverride, HelmValuesFiles: input.HelmValuesFiles, HelmValuesYAML: input.HelmValuesYAML, SyncPolicy: input.SyncPolicy, PollSeconds: input.PollSeconds}
	apps := make([]store.Application, 0, len(input.Targets))
	seenClusters, seenNames := map[string]bool{}, map[string]bool{}
	for _, target := range input.Targets {
		target.Name = strings.TrimSpace(target.Name)
		if !resourceNamePattern.MatchString(target.Name) || target.ClusterID == "" || len(target.Namespaces) == 0 {
			writeError(w, http.StatusBadRequest, "each cluster target needs a valid application name and at least one namespace")
			return
		}
		if seenNames[target.Name] {
			writeError(w, http.StatusBadRequest, "target application names must be unique")
			return
		}
		seenNames[target.Name] = true
		if seenClusters[target.ClusterID] {
			writeError(w, http.StatusBadRequest, "each cluster can only appear once in a deployment group")
			return
		}
		seenClusters[target.ClusterID] = true
		namespaceNames := make([]string, 0, len(target.Namespaces))
		for _, namespace := range target.Namespaces {
			namespaceNames = append(namespaceNames, namespace.Namespace)
		}
		targetManifestPath, namespaceManifestPaths, pathErr := validateKustomizeManifestPaths(input.Renderer, target.TargetManifestPath, target.NamespaceManifestPaths, namespaceNames)
		if pathErr != nil {
			writeError(w, http.StatusBadRequest, "target "+target.Name+": "+pathErr.Error())
			return
		}
		target.TargetManifestPath, target.NamespaceManifestPaths = targetManifestPath, namespaceManifestPaths
		targetFiles, targetValues, err := validateHelmValuesInput(input.Renderer, target.TargetHelmValuesFiles, target.TargetHelmValuesYAML)
		if err != nil {
			writeError(w, http.StatusBadRequest, "target "+target.Name+": "+err.Error())
			return
		}
		canUseCluster, clusterErr := s.Store.WorkspaceCanUseCluster(r.Context(), input.WorkspaceID, target.ClusterID)
		if clusterErr != nil || !canUseCluster {
			writeError(w, http.StatusBadRequest, "target cluster not found")
			return
		}
		bindings := make([]store.NamespaceBinding, 0, len(target.Namespaces))
		namespaceValues := make(map[string]core.HelmValuesOverride, len(target.Namespaces))
		namespaceManifestPathOverrides := make(map[string]string, len(target.NamespaceManifestPaths))
		seenNamespaces := map[string]bool{}
		for _, namespace := range target.Namespaces {
			namespace.Namespace = strings.TrimSpace(namespace.Namespace)
			if !namespaceNamePattern.MatchString(namespace.Namespace) || seenNamespaces[namespace.Namespace] {
				writeError(w, http.StatusBadRequest, "target namespace names must be valid and unique within their cluster")
				return
			}
			seenNamespaces[namespace.Namespace] = true
			files, values, err := validateHelmValuesInput(input.Renderer, namespace.Files, namespace.YAML)
			if err != nil {
				writeError(w, http.StatusBadRequest, "namespace "+namespace.Namespace+": "+err.Error())
				return
			}
			binding, err := s.Store.NamespaceBinding(r.Context(), input.WorkspaceID, target.ClusterID, namespace.Namespace)
			if err != nil {
				writeError(w, http.StatusBadRequest, "every target namespace must have a workspace binding")
				return
			}
			bindings = append(bindings, binding)
			namespaceValues[namespace.Namespace] = core.HelmValuesOverride{Files: files, YAML: values}
			if path := target.NamespaceManifestPaths[namespace.Namespace]; path != "" {
				namespaceManifestPathOverrides[namespace.Namespace] = path
			}
		}
		if len(namespaceValues) == 0 {
			writeError(w, http.StatusBadRequest, "each cluster target must include at least one bound namespace")
			return
		}
		apps = append(apps, store.Application{ID: store.NewID(), WorkspaceID: input.WorkspaceID, Name: target.Name, SourceID: input.SourceID, Revision: input.Revision, ManifestPath: input.ManifestPath, TargetManifestPath: target.TargetManifestPath, NamespaceManifestPaths: namespaceManifestPathOverrides, Renderer: input.Renderer, KustomizeHelmEnabled: input.KustomizeHelmEnabled, KustomizeNamespaceOverride: input.KustomizeNamespaceOverride, ApplicationGroupID: group.ID, HelmValuesFiles: input.HelmValuesFiles, HelmValuesYAML: input.HelmValuesYAML, TargetHelmValuesFiles: targetFiles, TargetHelmValuesYAML: targetValues, NamespaceHelmValues: namespaceValues, ClusterID: target.ClusterID, Namespaces: bindings, SyncPolicy: input.SyncPolicy, PollSeconds: input.PollSeconds, Health: "unknown"})
	}
	if err := s.Store.CreateApplicationGroup(r.Context(), group, apps); err != nil {
		writeError(w, http.StatusConflict, "could not create deployment group; verify its name and target application names are unique")
		return
	}
	details := applicationGroupAuditDetails(group)
	details["targetCount"] = len(apps)
	details["applicationIds"] = applicationIDs(apps)
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application_group.created", "application_group", group.ID, details)
	writeJSON(w, http.StatusCreated, map[string]any{"group": group, "applications": apps})
}

func applicationIDs(apps []store.Application) []string {
	ids := make([]string, 0, len(apps))
	for _, app := range apps {
		ids = append(ids, app.ID)
	}
	return ids
}

func applicationGroupAuditDetails(group store.ApplicationGroup) map[string]any {
	return map[string]any{
		"name": group.Name, "workspaceId": group.WorkspaceID, "sourceId": group.SourceID,
		"revision": group.Revision, "manifestPath": group.ManifestPath, "renderer": group.Renderer,
		"syncPolicy": group.SyncPolicy, "pollSeconds": group.PollSeconds,
		"kustomizeHelmEnabled": group.KustomizeHelmEnabled, "kustomizeNamespaceOverride": group.KustomizeNamespaceOverride,
		"helmValuesFiles": group.HelmValuesFiles, "helmValuesConfigured": group.HelmValuesYAML != "",
	}
}

func (s *Server) getApplicationGroup(w http.ResponseWriter, r *http.Request) {
	group, err := s.Store.ApplicationGroupByID(r.Context(), r.PathValue("groupID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "deployment group not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, group.WorkspaceID, "viewer") {
		return
	}
	apps, err := s.Store.ApplicationsByGroupID(r.Context(), group.ID)
	if err != nil {
		writeStoreError(w, "could not load deployment targets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group, "applications": apps})
}

func (s *Server) updateApplicationGroup(w http.ResponseWriter, r *http.Request) {
	group, err := s.Store.ApplicationGroupByID(r.Context(), r.PathValue("groupID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "deployment group not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, group.WorkspaceID, "owner") {
		return
	}
	var input struct {
		SourceID                   string   `json:"sourceId"`
		Revision                   string   `json:"revision"`
		ManifestPath               string   `json:"manifestPath"`
		KustomizeHelmEnabled       bool     `json:"kustomizeHelmEnabled"`
		KustomizeNamespaceOverride bool     `json:"kustomizeNamespaceOverride"`
		HelmValuesFiles            []string `json:"helmValuesFiles"`
		HelmValuesYAML             string   `json:"helmValuesYaml"`
		SyncPolicy                 string   `json:"syncPolicy"`
		PollSeconds                int      `json:"pollSeconds"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Revision = strings.TrimSpace(input.Revision)
	input.ManifestPath = path.Clean(strings.TrimSpace(input.ManifestPath))
	if input.Revision == "" || len(input.Revision) > 256 || strings.ContainsAny(input.Revision, " \t\r\n\x00") || input.ManifestPath == "." || input.ManifestPath == ".." || strings.HasPrefix(input.ManifestPath, "../") || path.IsAbs(input.ManifestPath) || strings.ContainsRune(input.ManifestPath, '\x00') {
		writeError(w, http.StatusBadRequest, "invalid Git revision or manifest path")
		return
	}
	canUseSource, sourceErr := s.Store.WorkspaceCanUseGitSource(r.Context(), group.WorkspaceID, input.SourceID)
	if sourceErr != nil || !canUseSource {
		writeError(w, http.StatusBadRequest, "Git source is not available to this workspace")
		return
	}
	if (input.KustomizeHelmEnabled || input.KustomizeNamespaceOverride) && group.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "Kustomize settings require the Kustomize renderer")
		return
	}
	files, values, err := validateHelmValuesInput(group.Renderer, input.HelmValuesFiles, input.HelmValuesYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.SyncPolicy != "manual" && input.SyncPolicy != "auto-safe" || input.PollSeconds < 30 || input.PollSeconds > 86400 {
		writeError(w, http.StatusBadRequest, "invalid reconciliation settings")
		return
	}
	group.SourceID, group.Revision, group.ManifestPath = input.SourceID, input.Revision, input.ManifestPath
	group.KustomizeHelmEnabled, group.KustomizeNamespaceOverride = input.KustomizeHelmEnabled, input.KustomizeNamespaceOverride
	group.HelmValuesFiles, group.HelmValuesYAML = files, values
	group.SyncPolicy, group.PollSeconds = input.SyncPolicy, input.PollSeconds
	if err := s.Store.UpdateApplicationGroup(r.Context(), group); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	apps, err := s.Store.ApplicationsByGroupID(r.Context(), group.ID)
	if err != nil {
		writeStoreError(w, "deployment group updated, but targets could not be loaded")
		return
	}
	details := applicationGroupAuditDetails(group)
	details["applicationIds"] = applicationIDs(apps)
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application_group.updated", "application_group", group.ID, details)
	writeJSON(w, http.StatusOK, map[string]any{"group": group, "applications": apps})
}
