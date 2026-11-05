package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

type profileUpgradePlan struct {
	ProfileID     string                    `json:"profileId"`
	SyncPolicy    string                    `json:"syncPolicy"`
	ApplicationID string                    `json:"applicationId"`
	ProfileName   string                    `json:"profileName"`
	FromRevision  int64                     `json:"fromRevision"`
	ToRevision    int64                     `json:"toRevision"`
	SourceSHA     string                    `json:"sourceSha"`
	Hash          string                    `json:"hash"`
	Changes       []publishing.RecipeChange `json:"changes"`
	Images        []profileImageChange      `json:"images"`
	Resume        bool                      `json:"resume"`
	UpToDate      bool                      `json:"upToDate"`
}
type profileImageChange struct {
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func (s *Server) listPublishingProfileVersions(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.PublishingProfile(r.Context(), r.PathValue("profileID"))
	if err != nil {
		writeError(w, 404, "publishing profile not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, p.WorkspaceID, "owner") {
		return
	}
	versions, err := s.Store.PublishingProfileVersions(r.Context(), p.ID)
	if err != nil {
		writeStoreError(w, "could not load profile revisions")
		return
	}
	writeJSON(w, 200, map[string]any{"items": versions})
}
func (s *Server) previewPublishingProfileUpgrade(w http.ResponseWriter, r *http.Request) {
	s.publishingProfileUpgrade(w, r, false)
}
func (s *Server) applyPublishingProfileUpgrade(w http.ResponseWriter, r *http.Request) {
	s.publishingProfileUpgrade(w, r, true)
}
func (s *Server) publishingProfileUpgrade(w http.ResponseWriter, r *http.Request, apply bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	app, err := s.Store.ApplicationByID(ctx, r.PathValue("applicationID"))
	if err != nil {
		writeError(w, 404, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "deployer") {
		return
	}
	if app.Decommissioning {
		writeError(w, 409, "application is being decommissioned")
		return
	}
	if app.Revision != "main" || app.ManifestPath != "deploy" {
		writeError(w, 409, "profile upgrades require the original managed publishing source")
		return
	}
	source, err := s.Store.GitSourceByID(ctx, app.SourceID)
	if err != nil || source.WorkspaceID != app.WorkspaceID {
		writeError(w, 404, "application source not found")
		return
	}
	publication, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, source.RepositoryURL)
	if err != nil || publication.ApplicationID != app.ID || publication.ProfileID == nil {
		writeError(w, 404, "application is not a profile-managed publication")
		return
	}
	p, err := s.Store.PublishingProfile(ctx, *publication.ProfileID)
	if err != nil || p.WorkspaceID != app.WorkspaceID {
		writeError(w, 404, "publishing profile not found")
		return
	}
	if !p.Enabled {
		writeError(w, 403, "publishing profile is disabled")
		return
	}
	if !s.requireWorkspaceCluster(w, r, app.WorkspaceID, app.ClusterID) {
		return
	}
	if len(app.Namespaces) != 1 {
		writeError(w, 409, "application destination changed")
		return
	}
	if _, err = s.Store.NamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, app.Namespaces[0].Namespace); err != nil {
		writeError(w, 403, "application namespace is no longer bound to this workspace")
		return
	}
	target := p.CurrentRevision
	expectedHash := ""
	if apply {
		var input struct {
			Revision int64  `json:"revision"`
			Hash     string `json:"hash"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		target, expectedHash = input.Revision, input.Hash
	}
	resume := target == publication.ProfileRevision && publication.PipelineURL == "" && strings.HasPrefix(publication.Fingerprint, "upgrade:")
	plan := profileUpgradePlan{ProfileID: p.ID, SyncPolicy: app.SyncPolicy, ApplicationID: app.ID, ProfileName: p.Name, FromRevision: publication.ProfileRevision, ToRevision: target, Changes: []publishing.RecipeChange{}, Images: []profileImageChange{}, Resume: resume}
	if target < publication.ProfileRevision || target > p.CurrentRevision || target <= 0 {
		writeError(w, 400, "select a newer published profile revision")
		return
	}
	if target == publication.ProfileRevision && !resume {
		if apply {
			writeJSON(w, 200, map[string]any{"applicationId": app.ID, "profileRevision": target, "pipelineUrl": publication.PipelineURL, "alreadyUpgraded": true})
			return
		}
		plan.UpToDate = true
		writeJSON(w, 200, plan)
		return
	}
	version, err := s.Store.PublishingProfileVersion(ctx, p.ID, target)
	if err != nil {
		writeError(w, 404, "profile revision not found")
		return
	}
	if version.TemplateVersion != publishing.Version {
		writeError(w, 409, "target recipe version is unsupported by this server")
		return
	}
	var old publishing.Config
	if err = json.Unmarshal(publication.Config, &old); err != nil {
		writeStoreError(w, "could not read application settings")
		return
	}
	next := old
	next.ManagedCompletion = true
	next.BaseImage = version.Settings.BaseImage
	next.BuilderImage = version.Settings.BuilderImage
	next.WebServerImage = version.Settings.WebServerImage
	next.RunnerTag = version.Settings.RunnerTag
	next.Exposure = version.Settings.Exposure
	next.ExposureLabels = version.Settings.ExposureLabels
	next.ExposureAnnotations = version.Settings.ExposureAnnotations
	next.IngressClass = version.Settings.IngressClass
	next.IngressTLS = version.Settings.IngressTLS
	next.TemplateVersion = version.Settings.TemplateVersion
	next.TemplateParameters = version.Settings.TemplateParameters
	next.RunnerServiceAccount = version.Settings.RunnerServiceAccount
	next.RunnerSCC = version.Settings.RunnerSCC
	next.BuilderSetup = version.Settings.BuilderSetup
	next.CAVariable = version.Settings.CAVariable
	next.BuildEngine = version.Settings.BuildEngine
	next.GitImage = version.Settings.GitImage
	next.PullSecret = version.Settings.PullSecret
	// Destination and secret policy remain application-owned during an upgrade.
	lookup, err := s.templateLookup(ctx, app.WorkspaceID)
	if err != nil {
		writeStoreError(w, "could not load templates")
		return
	}
	resolved, err := version.Settings.ResolveWith(lookup, app.Name, old.EntryPoint)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	choices, err := publishing.ServiceChoices(old.Services)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	catalog, err := publishing.ParseServiceCatalog(version.Settings.ServiceCatalog)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	next.Services, err = catalog.Resolve(choices)
	if err != nil {
		writeError(w, 409, "the new profile must retain the application's selected services: "+err.Error())
		return
	}
	if err := publishing.PreserveDatabaseCredentials(old, &next); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	resolved.Services = next.Services
	resolved.BuildCommand, resolved.OutputDir = old.BuildCommand, old.OutputDir
	resolved.ManagedCompletion = true
	if resolved != next || old.Namespace != app.Namespaces[0].Namespace {
		writeError(w, 409, "this revision changes locked infrastructure; destination migrations require a new profile/application")
		return
	}
	secrets, err := s.loadPublishingSecrets(r, p)
	if err != nil {
		writeError(w, 409, "publishing credentials need administrator attention")
		return
	}
	git := publishing.GitLab{URL: publication.GitLabURL, Token: secrets.GitLabToken, HTTP: s.PublishingHTTP}
	if err := git.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	origin, projectPath, err := publishing.RepositoryLocation(publication.RepositoryURL)
	if err != nil || origin != strings.TrimRight(p.Settings.GitLabURL, "/") || !strings.HasPrefix(projectPath, p.Settings.Group+"/") {
		writeError(w, 409, "application repository no longer matches its publishing profile")
		return
	}
	// Serialize against ZIP uploads using the same repository advisory lock.
	lock, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		writeStoreError(w, "could not start profile upgrade")
		return
	}
	defer lock.Rollback()
	markerSum := sha256.Sum256([]byte(app.WorkspaceID + "\n" + strings.TrimRight(publication.GitLabURL, "/") + "\n" + publication.Group + "/" + app.Name))
	marker := "JustCD managed publication " + hex.EncodeToString(markerSum[:])
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
		writeError(w, 409, "the managed repository default branch changed; restore main before upgrading its publishing recipe")
		return
	}
	if snapshot.Project.ID != publication.ProjectID || snapshot.Project.Description != marker {
		writeError(w, 409, "repository is not the original JustCD managed publication")
		return
	}
	plan.SourceSHA = snapshot.SHA
	files := []publishing.File{}
	for _, entry := range snapshot.Entries {
		if strings.HasPrefix(entry.Path, "source/") {
			if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755" && entry.Mode != "") {
				if entry.Type == "tree" {
					continue
				}
				writeError(w, 409, "source contains links or submodules; review the repository manually")
				return
			}
			files = append(files, publishing.File{Path: entry.Path})
		}
	}
	if err = s.attachTemplate(ctx, app.WorkspaceID, &next); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	generated, err := publishing.Generate(next, files)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	for _, file := range generated {
		if strings.HasPrefix(file.Path, "source/") || strings.HasPrefix(file.Path, "deploy/") {
			continue
		}
		before, last, exists, e := git.RepositoryFile(ctx, snapshot.Project.ID, file.Path, snapshot.SHA)
		if e != nil {
			writeError(w, 502, e.Error())
			return
		}
		if len(before)+len(file.Content) > 200000 {
			writeError(w, 409, "managed build file is too large for an upgrade review; review it in GitLab")
			return
		}
		if before != file.Content {
			plan.Changes = append(plan.Changes, publishing.RecipeChange{Path: file.Path, Before: before, After: file.Content, Exists: exists, LastCommitID: last})
		}
	}
	for _, change := range []profileImageChange{{"Deployment update image", old.GitImage, next.GitImage}, {"Build engine", old.BuildEngine, next.BuildEngine}, {"CI CA variable", old.CAVariable, next.CAVariable}, {"Runtime base image", old.BaseImage, next.BaseImage}, {"CI builder image", old.BuilderImage, next.BuilderImage}, {"Static web server image", old.WebServerImage, next.WebServerImage}, {"Runner tag", old.RunnerTag, next.RunnerTag}, {"Exposure type", old.Exposure, next.Exposure}, {"Exposure labels", old.ExposureLabels, next.ExposureLabels}, {"Exposure annotations", old.ExposureAnnotations, next.ExposureAnnotations}, {"Ingress class", old.IngressClass, next.IngressClass}, {"Ingress TLS", old.IngressTLS, next.IngressTLS}, {"Application services", old.Services, next.Services}, {"Template parameters", old.TemplateParameters, next.TemplateParameters}, {"Template version", templateLabel(old.TemplateVersion), templateLabel(next.TemplateVersion)}, {"Runner service account", old.RunnerServiceAccount, next.RunnerServiceAccount}, {"Runner SCC", old.RunnerSCC, next.RunnerSCC}, {"Builder setup command", old.BuilderSetup, next.BuilderSetup}, {"Image pull secret", old.PullSecret, next.PullSecret}} {
		if change.Before != change.After {
			plan.Images = append(plan.Images, change)
		}
	}
	data, _ := json.Marshal(struct {
		Plan   profileUpgradePlan
		Config publishing.Config
	}{plan, next})
	sum := sha256.Sum256(data)
	plan.Hash = hex.EncodeToString(sum[:])
	if !apply {
		writeJSON(w, 200, plan)
		return
	}
	if expectedHash == "" || expectedHash != plan.Hash {
		writeError(w, 409, "application, source, or upgrade plan changed; review the upgrade again")
		return
	}
	// Recheck the application after acquiring the upload lock.
	current, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, source.RepositoryURL)
	if err != nil || current.ProfileRevision != publication.ProfileRevision || string(current.Config) != string(publication.Config) {
		writeError(w, 409, "application changed; review the upgrade again")
		return
	}
	active, err := s.Store.PublishingProfile(ctx, p.ID)
	if err != nil || !active.Enabled {
		writeError(w, 403, "publishing profile was disabled")
		return
	}
	if next.SecretMode == "variables" {
		if err := git.SetVariables(ctx, snapshot.Project, []publishing.Variable{{Key: "REGISTRY_USER", Value: secrets.RegistryUser, Protected: true, Raw: true}, {Key: "REGISTRY_PASSWORD", Value: secrets.RegistryPassword, Masked: true, Protected: true, Raw: true}}); err != nil {
			writeError(w, 502, "could not refresh registry credentials: "+err.Error())
			return
		}
	}
	if err := git.CommitRecipeUpgrade(ctx, snapshot, plan.Changes, plan.Hash); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if err := s.Store.UpgradePublishingRevision(ctx, app, publication, next, target, "upgrade:"+plan.Hash); err != nil {
		writeError(w, 409, "recipes committed but application revision could not be recorded; review and retry: "+err.Error())
		return
	}
	_ = s.Store.Audit(ctx, currentUser(r).ID, "application.profile_upgraded", "application", app.ID, map[string]any{"profileId": p.ID, "fromRevision": publication.ProfileRevision, "toRevision": target, "sourceSha": snapshot.SHA, "planHash": plan.Hash})
	pipeline, err := git.StartPipeline(ctx, publication.ProjectID)
	if err != nil {
		writeError(w, 502, "profile revision updated, but the build could not start; review the pending upgrade again to retry starting CI")
		return
	}
	link, linkErr := url.Parse(pipeline)
	originURL, _ := url.Parse(publication.GitLabURL)
	if linkErr != nil || link.Scheme != "https" || link.Host != originURL.Host {
		writeError(w, 502, "build started but GitLab returned an unexpected link; inspect the project before retrying")
		return
	}
	if err := s.Store.SetPublishingPipeline(ctx, app.ID, pipeline); err != nil {
		writeStoreError(w, "build started but its URL could not be saved; inspect GitLab before retrying")
		return
	}
	writeJSON(w, 200, map[string]any{"applicationId": app.ID, "profileRevision": target, "pipelineUrl": pipeline})
}

func templateLabel(version int) string {
	if version <= 0 {
		return "built-in"
	}
	return "custom v" + strconv.Itoa(version)
}
