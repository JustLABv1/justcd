package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// A manually published application keeps its build files in Git, written once
// at publication. These endpoints let an owner change its build setup later
// (package sources) and bind it to a publishing profile, always through a
// reviewed diff of the generated files.

type managedPublication struct {
	app         store.Application
	source      store.GitSource
	publication store.PublishedApplication
	config      publishing.Config
}

// loadManagedPublication resolves an application to its managed publication.
// A non-zero status describes why it cannot be used.
func (s *Server) loadManagedPublication(ctx context.Context, w http.ResponseWriter, r *http.Request, minimum string) (managedPublication, bool) {
	var m managedPublication
	app, err := s.Store.ApplicationByID(ctx, r.PathValue("applicationID"))
	if err != nil {
		writeError(w, 404, "application not found")
		return m, false
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, minimum) {
		return m, false
	}
	if app.Decommissioning {
		writeError(w, 409, "application is being decommissioned")
		return m, false
	}
	if app.Revision != "main" || app.ManifestPath != "deploy" {
		writeError(w, 409, "this requires the original managed publishing source")
		return m, false
	}
	if !s.requireWorkspaceCluster(w, r, app.WorkspaceID, app.ClusterID) {
		return m, false
	}
	source, err := s.Store.GitSourceByID(ctx, app.SourceID)
	if err != nil || source.WorkspaceID != app.WorkspaceID {
		writeError(w, 404, "application source not found")
		return m, false
	}
	publication, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, source.RepositoryURL)
	if err != nil || publication.ApplicationID != app.ID {
		writeError(w, 404, "application is not a managed publication")
		return m, false
	}
	if json.Unmarshal(publication.Config, &m.config) != nil {
		writeStoreError(w, "could not read application settings")
		return m, false
	}
	if len(app.Namespaces) != 1 {
		writeError(w, 409, "application destination changed")
		return m, false
	}
	m.app, m.source, m.publication = app, source, publication
	return m, true
}

func publicationMarker(app store.Application, publication store.PublishedApplication) string {
	sum := sha256.Sum256([]byte(app.WorkspaceID + "\n" + strings.TrimRight(publication.GitLabURL, "/") + "\n" + publication.Group + "/" + app.Name))
	return "JustCD managed publication " + hex.EncodeToString(sum[:])
}

// recipeChanges generates the build files for next from the repository's current
// source and returns the ones that differ from Git.
func (s *Server) recipeChanges(ctx context.Context, git publishing.GitLab, snapshot publishing.RepositorySnapshot, next publishing.Config) ([]publishing.RecipeChange, int, string) {
	files := []publishing.File{}
	for _, entry := range snapshot.Entries {
		if strings.HasPrefix(entry.Path, "source/") {
			if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755" && entry.Mode != "") {
				if entry.Type == "tree" {
					continue
				}
				return nil, 409, "source contains links or submodules; review the repository manually"
			}
			files = append(files, publishing.File{Path: entry.Path})
		}
	}
	generated, err := publishing.Generate(next, files)
	if err != nil {
		return nil, 400, err.Error()
	}
	changes := []publishing.RecipeChange{}
	for _, file := range generated {
		if strings.HasPrefix(file.Path, "source/") || strings.HasPrefix(file.Path, "deploy/") {
			continue
		}
		before, last, exists, e := git.RepositoryFile(ctx, snapshot.Project.ID, file.Path, snapshot.SHA)
		if e != nil {
			return nil, 502, e.Error()
		}
		if len(before)+len(file.Content) > 200000 {
			return nil, 409, "managed build file is too large for a review; review it in GitLab"
		}
		if before != file.Content {
			changes = append(changes, publishing.RecipeChange{Path: file.Path, Before: before, After: file.Content, Exists: exists, LastCommitID: last})
		}
	}
	return changes, 0, ""
}

type buildSetupPlan struct {
	ApplicationID string                    `json:"applicationId"`
	SourceSHA     string                    `json:"sourceSha"`
	Hash          string                    `json:"hash"`
	Changes       []publishing.RecipeChange `json:"changes"`
	Images        []profileImageChange      `json:"images"`
	UpToDate      bool                      `json:"upToDate"`
}

func (s *Server) previewBuildSetup(w http.ResponseWriter, r *http.Request) { s.buildSetup(w, r, false) }
func (s *Server) applyBuildSetup(w http.ResponseWriter, r *http.Request)   { s.buildSetup(w, r, true) }

func (s *Server) buildSetup(w http.ResponseWriter, r *http.Request, apply bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	var in struct {
		GitLabToken    string `json:"gitlabToken"`
		PackageSources string `json:"packageSources"`
		PackagePolicy  string `json:"packagePolicy"`
		// TemplateParameters changes the template's non-secret parameters; nil keeps them.
		TemplateParameters *string `json:"templateParameters"`
		PackageUser        string  `json:"packageUser"`
		PackageToken       string  `json:"packageToken"`
		Hash               string  `json:"hash"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid build setup request")
		return
	}
	m, ok := s.loadManagedPublication(ctx, w, r, "owner")
	if !ok {
		return
	}
	if m.publication.ProfileID != nil {
		writeError(w, 409, "this application uses a publishing profile; update the profile instead")
		return
	}
	old := m.config
	next := old
	next.PackageSources, next.PackagePolicy = strings.TrimSpace(in.PackageSources), in.PackagePolicy
	if in.TemplateParameters != nil {
		next.TemplateParameters = strings.TrimSpace(*in.TemplateParameters)
	}
	if err := next.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if apply && next.SecretMode == "variables" && next.PackagesAuthenticated() && (in.PackageUser == "" || len(in.PackageToken) < 8 || strings.ContainsAny(in.PackageToken, " \r\n\t")) {
		writeError(w, 400, "package credentials (user and a token of at least eight characters) are required for authenticated package sources")
		return
	}
	if next.SecretMode == "vault" && (in.PackageUser != "" || in.PackageToken != "") {
		writeError(w, 400, "Vault mode reads package credentials from Vault, not from the request")
		return
	}
	if err := s.attachTemplate(ctx, m.app.WorkspaceID, &next); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	git := publishing.GitLab{URL: m.publication.GitLabURL, Token: in.GitLabToken, HTTP: s.PublishingHTTP}
	if err := git.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	origin, projectPath, err := publishing.RepositoryLocation(m.publication.RepositoryURL)
	if err != nil || origin != strings.TrimRight(m.publication.GitLabURL, "/") {
		writeError(w, 409, "application repository does not match its recorded GitLab location")
		return
	}
	lock, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		writeStoreError(w, "could not start the build setup update")
		return
	}
	defer lock.Rollback()
	marker := publicationMarker(m.app, m.publication)
	if apply {
		var acquired bool
		if err = lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, marker).Scan(&acquired); err != nil || !acquired {
			writeError(w, 409, "application is already publishing; retry later")
			return
		}
	}
	snapshot, err := git.InspectRepository(ctx, projectPath, "main")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if snapshot.Project.DefaultBranch != "main" {
		writeError(w, 409, "the managed repository default branch changed; restore main before updating its build setup")
		return
	}
	if snapshot.Project.ID != m.publication.ProjectID || snapshot.Project.Description != marker {
		writeError(w, 409, "repository is not the original JustCD managed publication")
		return
	}
	changes, status, message := s.recipeChanges(ctx, git, snapshot, next)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	plan := buildSetupPlan{ApplicationID: m.app.ID, SourceSHA: snapshot.SHA, Changes: changes, Images: []profileImageChange{}}
	for _, change := range []profileImageChange{{"Package sources", old.PackageSources, next.PackageSources}, {"Package policy", old.PackagePolicy, next.PackagePolicy}, {"Template parameters", old.TemplateParameters, next.TemplateParameters}} {
		if change.Before != change.After {
			plan.Images = append(plan.Images, change)
		}
	}
	plan.UpToDate = len(plan.Changes) == 0 && len(plan.Images) == 0
	data, _ := json.Marshal(struct {
		Plan   buildSetupPlan
		Config publishing.Config
	}{plan, next})
	sum := sha256.Sum256(data)
	plan.Hash = hex.EncodeToString(sum[:])
	if !apply {
		writeJSON(w, 200, plan)
		return
	}
	if plan.UpToDate {
		writeJSON(w, 200, map[string]any{"applicationId": m.app.ID, "alreadyApplied": true, "pipelineUrl": m.publication.PipelineURL})
		return
	}
	if in.Hash == "" || in.Hash != plan.Hash {
		writeError(w, 409, "application, source, or build setup changed; review it again")
		return
	}
	current, err := s.Store.PublishedApplication(ctx, m.app.WorkspaceID, m.source.RepositoryURL)
	if err != nil || current.ProfileID != nil || string(current.Config) != string(m.publication.Config) {
		writeError(w, 409, "application changed; review the build setup again")
		return
	}
	if next.SecretMode == "variables" && next.PackagesAuthenticated() {
		variables := []publishing.Variable{
			{Key: "JUSTCD_PKG_USER", Value: in.PackageUser, Protected: true, Raw: true},
			{Key: "JUSTCD_PKG_TOKEN", Value: in.PackageToken, Masked: true, Protected: true, Raw: true},
		}
		if err := git.SetVariables(ctx, snapshot.Project, variables); err != nil {
			writeError(w, 502, "could not set the package credentials in GitLab: "+err.Error())
			return
		}
	}
	if err := git.CommitRecipeUpgrade(ctx, snapshot, plan.Changes, plan.Hash); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if err := s.Store.UpdatePublishedConfig(ctx, m.publication, next, "upgrade:"+plan.Hash); err != nil {
		writeError(w, 409, "build files committed but the application settings could not be recorded; review and retry: "+err.Error())
		return
	}
	_ = s.Store.Audit(ctx, currentUser(r).ID, "application.build_setup_updated", "application", m.app.ID, map[string]any{"sourceSha": snapshot.SHA, "planHash": plan.Hash, "changedFiles": len(plan.Changes)})
	pipeline, err := git.StartPipeline(ctx, m.publication.ProjectID)
	if err != nil {
		writeError(w, 502, "build setup updated, but the build could not start; use Upload new version or start the pipeline in GitLab")
		return
	}
	link, linkErr := url.Parse(pipeline)
	originURL, _ := url.Parse(m.publication.GitLabURL)
	if linkErr != nil || link.Scheme != "https" || link.Host != originURL.Host {
		writeError(w, 502, "build started but GitLab returned an unexpected link; inspect the project before retrying")
		return
	}
	if err := s.Store.SetPublishingPipeline(ctx, m.app.ID, pipeline); err != nil {
		writeStoreError(w, "build started but its URL could not be saved; inspect GitLab")
		return
	}
	writeJSON(w, 200, map[string]any{"applicationId": m.app.ID, "pipelineUrl": pipeline})
}

// ---- Convert to a publishing profile ---------------------------------------

type adoptionDifference struct {
	Field   string `json:"field"`
	App     string `json:"app"`
	Profile string `json:"profile"`
}

// configDifferences lists the fields that differ between two configurations.
func configDifferences(app, profile publishing.Config) []adoptionDifference {
	a, p := reflect.ValueOf(app), reflect.ValueOf(profile)
	t := a.Type()
	out := []adoptionDifference{}
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" || t.Field(i).Type.Kind() == reflect.Ptr {
			continue
		}
		if !reflect.DeepEqual(a.Field(i).Interface(), p.Field(i).Interface()) {
			out = append(out, adoptionDifference{Field: tag, App: fmt.Sprint(a.Field(i).Interface()), Profile: fmt.Sprint(p.Field(i).Interface())})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// adoptPublishingProfile binds a manually published application to a profile.
// It succeeds only when the application already sits where the profile would
// put it, because profile updates keep the destination locked. The profile's
// build settings are then adopted through the usual reviewed profile update.
func (s *Server) adoptPublishingProfile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	var in struct {
		ProfileID string `json:"profileId"`
		DryRun    bool   `json:"dryRun"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	m, ok := s.loadManagedPublication(ctx, w, r, "owner")
	if !ok {
		return
	}
	if m.publication.ProfileID != nil {
		writeError(w, 409, "this application already uses a publishing profile")
		return
	}
	p, err := s.Store.PublishingProfile(ctx, in.ProfileID)
	if err != nil || p.WorkspaceID != m.app.WorkspaceID {
		writeError(w, 404, "publishing profile not found")
		return
	}
	if !p.Enabled {
		writeError(w, 409, "publishing profile is disabled")
		return
	}
	version, err := s.Store.PublishingProfileVersion(ctx, p.ID, p.CurrentRevision)
	if err != nil {
		writeError(w, 404, "profile revision not found")
		return
	}
	lookup, err := s.templateLookup(ctx, m.app.WorkspaceID)
	if err != nil {
		writeStoreError(w, "could not load templates")
		return
	}
	reasons := []adoptionDifference{}
	old := m.config
	resolved, err := version.Settings.ResolveWith(lookup, m.app.Name, old.EntryPoint)
	if err != nil {
		reasons = append(reasons, adoptionDifference{Field: "profile", App: "", Profile: err.Error()})
	} else {
		// The same view a profile update takes: build settings come from the
		// profile, everything else stays with the application.
		next := old
		next.ManagedCompletion = true
		next.BaseImage, next.BuilderImage, next.WebServerImage = version.Settings.BaseImage, version.Settings.BuilderImage, version.Settings.WebServerImage
		next.RunnerTag, next.Exposure, next.ExposureLabels, next.ExposureAnnotations = version.Settings.RunnerTag, version.Settings.Exposure, version.Settings.ExposureLabels, version.Settings.ExposureAnnotations
		next.IngressClass, next.IngressTLS = version.Settings.IngressClass, version.Settings.IngressTLS
		next.TemplateVersion, next.TemplateParameters = version.Settings.TemplateVersion, version.Settings.TemplateParameters
		next.RunnerServiceAccount, next.RunnerSCC, next.BuilderSetup = version.Settings.RunnerServiceAccount, version.Settings.RunnerSCC, version.Settings.BuilderSetup
		next.PackageSources, next.PackagePolicy = version.Settings.PackageSources, version.Settings.PackagePolicy
		next.CAVariable, next.BuildEngine, next.GitImage, next.PullSecret = version.Settings.CAVariable, version.Settings.BuildEngine, version.Settings.GitImage, version.Settings.PullSecret
		resolved.Services = old.Services
		resolved.BuildCommand, resolved.OutputDir = old.BuildCommand, old.OutputDir
		resolved.ManagedCompletion = true
		reasons = append(reasons, configDifferences(next, resolved)...)
	}
	if p.Settings.ClusterID != m.app.ClusterID {
		reasons = append(reasons, adoptionDifference{Field: "cluster", App: m.app.ClusterID, Profile: p.Settings.ClusterID})
	}
	if _, err := s.Store.NamespaceBinding(ctx, m.app.WorkspaceID, m.app.ClusterID, m.app.Namespaces[0].Namespace); err != nil {
		reasons = append(reasons, adoptionDifference{Field: "namespace binding", App: m.app.Namespaces[0].Namespace, Profile: "not bound to this workspace"})
	}
	origin, projectPath, locErr := publishing.RepositoryLocation(m.publication.RepositoryURL)
	if locErr != nil || origin != strings.TrimRight(p.Settings.GitLabURL, "/") || !strings.HasPrefix(projectPath, p.Settings.Group+"/") {
		reasons = append(reasons, adoptionDifference{Field: "repository", App: m.publication.RepositoryURL, Profile: strings.TrimRight(p.Settings.GitLabURL, "/") + "/" + p.Settings.Group + "/…"})
	}
	result := map[string]any{"profileId": p.ID, "profileName": p.Name, "compatible": len(reasons) == 0, "differences": reasons}
	if len(reasons) > 0 || in.DryRun {
		result["applied"] = false
		writeJSON(w, 200, result)
		return
	}
	if err := s.Store.AdoptPublishingProfile(ctx, m.publication, p.ID, p.CurrentRevision); err != nil {
		writeError(w, 409, "could not convert the application: "+err.Error())
		return
	}
	_ = s.Store.Audit(ctx, currentUser(r).ID, "application.profile_adopted", "application", m.app.ID, map[string]any{"profileId": p.ID, "profileRevision": p.CurrentRevision})
	result["applied"] = true
	writeJSON(w, 200, result)
}
