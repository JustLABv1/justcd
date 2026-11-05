package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func findTemplate(id string) (publishing.Template, bool) {
	for _, t := range publishing.Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return publishing.Template{}, false
}

// attachTemplate resolves a pinned custom template version into the config so
// generation renders the workspace's files instead of the built-in ones.
func (s *Server) attachTemplate(ctx context.Context, workspace string, c *publishing.Config) error {
	c.TemplateFiles = nil
	if c.TemplateVersion <= 0 {
		return nil
	}
	if c.TemplateID == "" {
		return errors.New("custom template versions require an application template")
	}
	v, err := s.Store.PublishingTemplateVersion(ctx, workspace, c.TemplateID, c.TemplateVersion)
	if err != nil {
		return errors.New("template version " + strconv.Itoa(c.TemplateVersion) + " of " + c.TemplateID + " is unavailable")
	}
	c.TemplateFiles = &publishing.TemplateFileSet{Files: v.Files}
	return nil
}

func (s *Server) listPublishingTemplates(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	versions, err := s.Store.PublishingTemplateVersions(r.Context(), workspace, "")
	if err != nil {
		writeStoreError(w, "could not load template versions")
		return
	}
	byTemplate := map[string][]store.PublishingTemplateVersion{}
	for _, v := range versions {
		byTemplate[v.TemplateID] = append(byTemplate[v.TemplateID], v)
	}
	items := []map[string]any{}
	for _, t := range publishing.Templates() {
		list := byTemplate[t.ID]
		if list == nil {
			list = []store.PublishingTemplateVersion{}
		}
		items = append(items, map[string]any{"template": t, "versions": list})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getPublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	t, ok := findTemplate(r.PathValue("templateID"))
	if !ok {
		writeError(w, 404, "application template not found")
		return
	}
	builtin, err := publishing.BuiltinTemplateFiles(t.Runtime)
	if err != nil {
		writeError(w, 500, "built-in template is unavailable")
		return
	}
	versions, err := s.Store.PublishingTemplateVersions(r.Context(), workspace, t.ID)
	if err != nil {
		writeStoreError(w, "could not load template versions")
		return
	}
	writeJSON(w, 200, map[string]any{"template": t, "builtin": builtin, "versions": versions, "fileOrder": publishing.TemplateFilePaths, "placeholders": publishing.TemplatePlaceholders()})
}

func (s *Server) getPublishingTemplateVersion(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	t, ok := findTemplate(r.PathValue("templateID"))
	version, err := strconv.Atoi(r.PathValue("version"))
	if !ok || err != nil || version < 0 {
		writeError(w, 404, "template version not found")
		return
	}
	if version == 0 {
		files, e := publishing.BuiltinTemplateFiles(t.Runtime)
		if e != nil {
			writeError(w, 500, "built-in template is unavailable")
			return
		}
		writeJSON(w, 200, store.PublishingTemplateVersion{WorkspaceID: workspace, TemplateID: t.ID, Files: files})
		return
	}
	v, e := s.Store.PublishingTemplateVersion(r.Context(), workspace, t.ID, version)
	if e != nil {
		writeError(w, 404, "template version not found")
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) createPublishingTemplateVersion(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	t, ok := findTemplate(r.PathValue("templateID"))
	if !ok {
		writeError(w, 404, "application template not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var in struct {
		Label string            `json:"label"`
		Files map[string]string `json:"files"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid template version")
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if len(in.Label) > 100 {
		writeError(w, 400, "the version label must be at most 100 characters")
		return
	}
	if err := publishing.ValidateTemplate(t.ID, in.Files); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	v, err := s.Store.CreatePublishingTemplateVersion(r.Context(), workspace, t.ID, in.Label, currentUser(r).ID, in.Files)
	if err != nil {
		writeError(w, 409, "could not save the template version: "+err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "publishing_template.version_created", "publishing_template", t.ID, map[string]any{"workspaceId": workspace, "version": v.Version, "label": v.Label})
	v.Files = nil
	writeJSON(w, 201, v)
}

// previewPublishingTemplate renders (but never saves) template files with the
// settings of a profile, or a sample configuration, so editors see the result.
func (s *Server) previewPublishingTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWorkspaceRole(w, r, r.PathValue("workspaceID"), "owner") {
		return
	}
	t, ok := findTemplate(r.PathValue("templateID"))
	if !ok {
		writeError(w, 404, "application template not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var in struct {
		Files       map[string]string           `json:"files"`
		Settings    *publishing.ProfileSettings `json:"settings"`
		HasRenvLock bool                        `json:"hasRenvLock"`
		Services    publishing.ServiceSelection `json:"services"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid preview request")
		return
	}
	if err := publishing.CheckTemplateFiles(in.Files); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	cfg, err := publishing.SampleConfig(t.ID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	if in.Settings != nil {
		settings := *in.Settings
		settings.TemplateID, settings.TemplateVersion = t.ID, 0
		if cfg, err = settings.Resolve("sample-app", ""); err != nil {
			writeError(w, 400, "profile settings: "+err.Error())
			return
		}
	}
	if in.Settings != nil {
		catalog, e := publishing.ParseServiceCatalog(in.Settings.ServiceCatalog)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		cfg.Services, e = catalog.Resolve(in.Services)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
	} else if in.Services.Database || len(in.Services.VaultSecrets) > 0 {
		writeError(w, 400, "select a profile to preview application services")
		return
	}
	cfg.TemplateFiles = &publishing.TemplateFileSet{Files: in.Files}
	out, err := publishing.RenderConfig(cfg, publishing.SourceFacts{HasRenvLock: in.HasRenvLock})
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"files": out})
}
