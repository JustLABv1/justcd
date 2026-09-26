package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (s *Server) listIgnoreSelectors(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	items, err := s.Store.IgnoreSelectors(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not load ignore selectors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createIgnoreSelector(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var rule core.IgnoreSelector
	if err := decodeJSON(w, r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID, rule.CreatedBy = store.NewID(), currentUser(r).ID
	rule.APIVersion, rule.Kind, rule.LabelKey, rule.LabelValue, rule.Reason = strings.TrimSpace(rule.APIVersion), strings.TrimSpace(rule.Kind), strings.TrimSpace(rule.LabelKey), strings.TrimSpace(rule.LabelValue), strings.TrimSpace(rule.Reason)
	if (rule.Kind == "") != (rule.APIVersion == "") || (rule.Kind == "" && rule.LabelKey == "") {
		writeError(w, http.StatusBadRequest, "provide API version and kind, or a label key")
		return
	}
	if rule.LabelKey == "" && rule.LabelValue != "" {
		writeError(w, http.StatusBadRequest, "label value requires a label key")
		return
	}
	if rule.LabelKey != "" && (len(validation.IsQualifiedName(rule.LabelKey)) > 0 || len(validation.IsValidLabelValue(rule.LabelValue)) > 0) {
		writeError(w, http.StatusBadRequest, "invalid Kubernetes label key or value")
		return
	}
	if len(rule.Reason) < 5 || len(rule.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "an ignore reason between 5 and 500 characters is required")
		return
	}
	if err := s.Store.ChangeIgnoreSelector(r.Context(), app.ID, currentUser(r).ID, rule, ""); err != nil {
		writeError(w, http.StatusConflict, "selector already exists or application is currently syncing")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.ignore_selector_created", "application", app.ID, rule)
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) deleteIgnoreSelector(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	id := r.PathValue("selectorID")
	if err := s.Store.ChangeIgnoreSelector(r.Context(), app.ID, currentUser(r).ID, core.IgnoreSelector{}, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "ignore selector not found")
			return
		}
		writeError(w, http.StatusConflict, "could not remove ignore selector while application is syncing")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.ignore_selector_deleted", "application", app.ID, map[string]string{"selectorId": id})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
