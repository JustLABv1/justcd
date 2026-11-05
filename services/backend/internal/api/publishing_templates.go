package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// workspaceTemplates returns the built-in templates followed by the
// workspace's own.
func (s *Server) workspaceTemplates(ctx context.Context, workspace string) ([]publishing.Template, error) {
	custom, err := s.Store.CustomPublishingTemplates(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return append(publishing.Templates(), custom...), nil
}

// templateLookup resolves built-in and workspace template IDs.
func (s *Server) templateLookup(ctx context.Context, workspace string) (publishing.TemplateLookup, error) {
	all, err := s.workspaceTemplates(ctx, workspace)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]publishing.Template, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}
	return func(id string) (publishing.Template, bool) { t, ok := byID[id]; return t, ok }, nil
}

// findTemplate loads one template of the workspace. A storage failure is
// reported as an error, an unknown ID as ok=false.
func (s *Server) findTemplate(ctx context.Context, workspace, id string) (publishing.Template, bool, error) {
	if t, ok := publishing.BuiltinLookup(id); ok {
		return t, true, nil
	}
	t, err := s.Store.CustomPublishingTemplate(ctx, workspace, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return publishing.Template{}, false, nil
		}
		return publishing.Template{}, false, err
	}
	return t, true, nil
}

// attachTemplate resolves the template of a configuration: its port and upload
// rules, and, for a pinned custom version, the files generation renders.
func (s *Server) attachTemplate(ctx context.Context, workspace string, c *publishing.Config) error {
	c.TemplateFiles = nil
	if c.TemplateID != "" {
		t, ok, err := s.findTemplate(ctx, workspace, c.TemplateID)
		if err != nil {
			return errors.New("could not load the application template")
		}
		if !ok {
			return errors.New("application template " + c.TemplateID + " is unavailable in this workspace")
		}
		if t.Runtime != c.Runtime {
			return errors.New("selected application template does not match the runtime")
		}
		c.Port = t.Port
		c.Layout = nil
		if len(t.Layout) > 0 {
			c.Layout = &publishing.TemplateLayout{Rules: t.Layout}
		}
		if t.Custom && c.TemplateVersion <= 0 {
			// Manual publishing has no profile to pin a version, so it takes the
			// latest; the version is then stored with the application.
			versions, err := s.Store.PublishingTemplateVersions(ctx, workspace, t.ID)
			if err != nil || len(versions) == 0 {
				return errors.New("the custom template " + t.Name + " has no versions")
			}
			c.TemplateVersion = versions[0].Version
		}
	}
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
	all, err := s.workspaceTemplates(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not load templates")
		return
	}
	items := []map[string]any{}
	for _, t := range all {
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
	t, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
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
	t, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	version, convErr := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
	if !ok || convErr != nil || version < 0 || (version == 0 && t.Custom) {
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
	t, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
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
	if err := publishing.ValidateTemplateFor(t, in.Files); err != nil {
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
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	t, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
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
	cfg := publishing.SampleConfigFor(t)
	if in.Settings != nil {
		lookup, e := s.templateLookup(r.Context(), workspace)
		if e != nil {
			writeStoreError(w, "could not load templates")
			return
		}
		settings := *in.Settings
		settings.TemplateID, settings.TemplateVersion = t.ID, 0
		if t.Custom {
			settings.TemplateVersion = 1 // the edited files replace the version below
		}
		if cfg, err = settings.ResolveWith(lookup, "sample-app", ""); err != nil {
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

// templateExchangeFormat identifies exported template files.
const templateExchangeFormat = "justcd.template/v1"

type templateDefinitionInput struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Category    string                  `json:"category"`
	Detail      string                  `json:"detail"`
	Kind        string                  `json:"kind"`
	Runtime     string                  `json:"runtime"`
	EntryPoint  string                  `json:"entryPoint"`
	Port        int                     `json:"port"`
	Layout      []publishing.LayoutRule `json:"layout"`
}

var defaultEntryPoints = map[string]string{publishing.RuntimeServer: "app.py", "static": ".", "python-static": "build_html.py"}

// buildCustomTemplate turns a definition into a validated template. Fields
// left empty are taken from base (a copied template) or from the kind.
func buildCustomTemplate(in templateDefinitionInput, base *publishing.Template) (publishing.Template, error) {
	t := publishing.Template{
		ID: strings.TrimSpace(in.ID), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		Category: strings.TrimSpace(in.Category), Detail: strings.TrimSpace(in.Detail), EntryPoint: strings.TrimSpace(in.EntryPoint),
		Port: in.Port, Layout: in.Layout, Custom: true, Version: publishing.Version,
	}
	if base != nil {
		t.Runtime, t.BaseTemplate = base.Runtime, base.ID
		if t.Description == "" {
			t.Description = base.Description
		}
		if t.Category == "" {
			t.Category = base.Category
		}
		if t.Detail == "" {
			t.Detail = base.Detail
		}
		if t.EntryPoint == "" {
			t.EntryPoint = base.EntryPoint
		}
		if t.Port == 0 {
			t.Port = base.Port
		}
		if t.Layout == nil {
			t.Layout = base.Layout
		}
	} else {
		switch {
		case in.Kind != "":
			runtime, ok := publishing.RuntimeForKind(in.Kind)
			if !ok {
				return t, errors.New("choose a template kind: server application, static website or static website with build")
			}
			t.Runtime = runtime
		default:
			t.Runtime = in.Runtime
		}
		if t.EntryPoint == "" {
			t.EntryPoint = defaultEntryPoints[t.Runtime]
		}
	}
	if t.Category == "" {
		t.Category = "Custom"
	}
	if t.Detail == "" {
		t.Detail = "Custom template"
		if t.Port > 0 {
			t.Detail = fmt.Sprintf("Custom template · port %d", t.Port)
		}
	}
	return t, publishing.ValidateCustomTemplate(t)
}

// createPublishingTemplate adds a workspace template: either a copy of an
// existing template (baseTemplate) or a new one from a kind, always with a
// first version.
func (s *Server) createPublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var in struct {
		templateDefinitionInput
		BaseTemplate string `json:"baseTemplate"`
		BaseVersion  int    `json:"baseVersion"`
		Label        string `json:"label"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid template")
		return
	}
	var base *publishing.Template
	var files map[string]string
	if in.BaseTemplate != "" {
		b, ok, err := s.findTemplate(r.Context(), workspace, in.BaseTemplate)
		if err != nil {
			writeStoreError(w, "could not load the base template")
			return
		}
		if !ok {
			writeError(w, 404, "base template not found")
			return
		}
		base = &b
		if b.Custom {
			version := in.BaseVersion
			if version <= 0 {
				versions, e := s.Store.PublishingTemplateVersions(r.Context(), workspace, b.ID)
				if e != nil || len(versions) == 0 {
					writeStoreError(w, "could not load the base template versions")
					return
				}
				version = versions[0].Version
			}
			v, e := s.Store.PublishingTemplateVersion(r.Context(), workspace, b.ID, version)
			if e != nil {
				writeError(w, 404, "base template version not found")
				return
			}
			files = v.Files
		} else if in.BaseVersion > 0 {
			v, e := s.Store.PublishingTemplateVersion(r.Context(), workspace, b.ID, in.BaseVersion)
			if e != nil {
				writeError(w, 404, "base template version not found")
				return
			}
			files = v.Files
		} else if files, err = publishing.BuiltinTemplateFiles(b.Runtime); err != nil {
			writeError(w, 500, "built-in template is unavailable")
			return
		}
	}
	t, err := buildCustomTemplate(in.templateDefinitionInput, base)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if files == nil {
		if files, err = publishing.BuiltinTemplateFiles(t.Runtime); err != nil {
			writeError(w, 500, "starter files are unavailable")
			return
		}
	}
	label := strings.TrimSpace(in.Label)
	if label == "" && base != nil {
		label = "Copied from " + base.Name
	}
	s.storeNewTemplate(w, r, workspace, t, label, files, map[string]any{"baseTemplate": in.BaseTemplate})
}

// storeNewTemplate validates the files against the template and saves both.
func (s *Server) storeNewTemplate(w http.ResponseWriter, r *http.Request, workspace string, t publishing.Template, label string, files map[string]string, audit map[string]any) {
	if len(label) > 100 {
		writeError(w, 400, "the version label must be at most 100 characters")
		return
	}
	if err := publishing.ValidateTemplateFor(t, files); err != nil {
		writeError(w, 400, "template files: "+err.Error())
		return
	}
	if _, err := s.Store.CreateCustomPublishingTemplate(r.Context(), workspace, t, label, currentUser(r).ID, files); err != nil {
		switch {
		case errors.Is(err, store.ErrTemplateExists):
			writeError(w, 409, err.Error())
		case strings.Contains(err.Error(), "maximum number"):
			writeError(w, 409, err.Error())
		default:
			writeStoreError(w, "could not save the template")
		}
		return
	}
	audit["workspaceId"], audit["runtime"] = workspace, t.Runtime
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "publishing_template.created", "publishing_template", t.ID, audit)
	writeJSON(w, 201, map[string]any{"template": t})
}

// updatePublishingTemplate edits a workspace template's definition. The ID and
// kind are fixed; new files are saved as versions.
func (s *Server) updatePublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	current, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
	if !ok || !current.Custom {
		writeError(w, 404, "custom application template not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in templateDefinitionInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid template")
		return
	}
	in.ID = current.ID
	in.Runtime, in.Kind = current.Runtime, ""
	if strings.TrimSpace(in.Category) == "" {
		in.Category = current.Category
	}
	if strings.TrimSpace(in.Detail) == "" {
		in.Detail = current.Detail
	}
	updated, err := buildCustomTemplate(in, nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated.BaseTemplate = current.BaseTemplate
	// An empty layout deliberately clears the rules; check the current files
	// still render with the new port and entry point.
	versions, err := s.Store.PublishingTemplateVersions(r.Context(), workspace, current.ID)
	if err == nil && len(versions) > 0 {
		var latest store.PublishingTemplateVersion
		if latest, err = s.Store.PublishingTemplateVersion(r.Context(), workspace, current.ID, versions[0].Version); err == nil {
			err = publishing.ValidateTemplateFor(updated, latest.Files)
		}
	}
	if err != nil {
		writeError(w, 400, "the latest template files do not work with these settings: "+err.Error())
		return
	}
	if err := s.Store.UpdateCustomPublishingTemplate(r.Context(), workspace, updated); err != nil {
		writeStoreError(w, "could not save the template")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "publishing_template.updated", "publishing_template", current.ID, map[string]any{"workspaceId": workspace})
	writeJSON(w, 200, map[string]any{"template": updated})
}

func (s *Server) deletePublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	id := r.PathValue("templateID")
	if _, builtin := publishing.BuiltinLookup(id); builtin {
		writeError(w, 400, "built-in templates cannot be deleted")
		return
	}
	if err := s.Store.DeleteCustomPublishingTemplate(r.Context(), workspace, id); err != nil {
		switch {
		case errors.Is(err, store.ErrTemplateInUse):
			writeError(w, 409, err.Error())
		case strings.Contains(err.Error(), "not found"):
			writeError(w, 404, "custom application template not found")
		default:
			writeStoreError(w, "could not delete the template")
		}
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "publishing_template.deleted", "publishing_template", id, map[string]any{"workspaceId": workspace})
	w.WriteHeader(http.StatusNoContent)
}

// templateExchange is the portable form of a template version.
type templateExchange struct {
	Format   string                  `json:"format"`
	Template templateDefinitionInput `json:"template"`
	Label    string                  `json:"label,omitempty"`
	Files    map[string]string       `json:"files"`
}

// exportPublishingTemplate returns a template version as a portable file.
func (s *Server) exportPublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	t, ok, err := s.findTemplate(r.Context(), workspace, r.PathValue("templateID"))
	if err != nil {
		writeStoreError(w, "could not load the application template")
		return
	}
	if !ok {
		writeError(w, 404, "application template not found")
		return
	}
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	var files map[string]string
	label := ""
	switch {
	case version > 0:
		v, e := s.Store.PublishingTemplateVersion(r.Context(), workspace, t.ID, version)
		if e != nil {
			writeError(w, 404, "template version not found")
			return
		}
		files, label = v.Files, v.Label
	case t.Custom:
		versions, e := s.Store.PublishingTemplateVersions(r.Context(), workspace, t.ID)
		if e != nil || len(versions) == 0 {
			writeStoreError(w, "could not load template versions")
			return
		}
		v, e := s.Store.PublishingTemplateVersion(r.Context(), workspace, t.ID, versions[0].Version)
		if e != nil {
			writeStoreError(w, "could not load the template version")
			return
		}
		files, label = v.Files, v.Label
	default:
		if files, err = publishing.BuiltinTemplateFiles(t.Runtime); err != nil {
			writeError(w, 500, "built-in template is unavailable")
			return
		}
	}
	doc := templateExchange{Format: templateExchangeFormat, Label: label, Files: files, Template: templateDefinitionInput{
		ID: t.ID, Name: t.Name, Description: t.Description, Category: t.Category, Detail: t.Detail,
		Runtime: t.Runtime, EntryPoint: t.EntryPoint, Port: t.Port, Layout: t.Layout,
	}}
	w.Header().Set("Content-Disposition", `attachment; filename="`+t.ID+`.justcd-template.json"`)
	writeJSON(w, 200, doc)
}

// importPublishingTemplate creates a workspace template from an exported file.
// The ID and name can be overridden to avoid clashes.
func (s *Server) importPublishingTemplate(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var raw templateExchange
	if err := decodeJSON(w, r, &raw); err != nil {
		writeError(w, 400, "invalid template file")
		return
	}
	if raw.Format != templateExchangeFormat {
		writeError(w, 400, "this is not a JustCD template file (expected format "+templateExchangeFormat+")")
		return
	}
	def := raw.Template
	def.Kind = ""
	if id := r.URL.Query().Get("id"); id != "" {
		def.ID = id
	}
	if name := r.URL.Query().Get("name"); name != "" {
		def.Name = name
	}
	t, err := buildCustomTemplate(def, nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.storeNewTemplate(w, r, workspace, t, strings.TrimSpace(raw.Label), raw.Files, map[string]any{"imported": true})
}
