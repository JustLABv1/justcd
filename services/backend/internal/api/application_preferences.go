package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) applicationPreferences(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	views, err := s.Store.ApplicationSavedViews(r.Context(), user.ID)
	if err != nil {
		writeStoreError(w, "could not load saved application views")
		return
	}
	favorites, err := s.Store.ApplicationFavorites(r.Context(), user)
	if err != nil {
		writeStoreError(w, "could not load favorite applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": views, "favoriteApplicationIds": favorites})
}
func (s *Server) saveApplicationView(w http.ResponseWriter, r *http.Request) {
	var input store.ApplicationSavedView
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 80 {
		writeError(w, http.StatusBadRequest, "view name must be between 1 and 80 characters")
		return
	}
	if err := input.Configuration.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.ID = r.PathValue("viewID")
	created := r.Method == http.MethodPost
	result, err := s.Store.SaveApplicationView(r.Context(), currentUser(r).ID, input, created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "saved view not found")
			return
		}
		var state interface{ SQLState() string }
		if errors.As(err, &state) && state.SQLState() == "23505" {
			writeError(w, http.StatusConflict, "a saved view with this name already exists")
			return
		}
		if errors.Is(err, store.ErrApplicationViewLimit) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreError(w, "could not save application view")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
func (s *Server) defaultApplicationView(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ViewID string `json:"viewId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetDefaultApplicationView(r.Context(), currentUser(r).ID, input.ViewID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "saved view not found")
		} else {
			writeStoreError(w, "could not select saved view")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
func (s *Server) deleteApplicationView(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteApplicationView(r.Context(), currentUser(r).ID, r.PathValue("viewID")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "saved view not found")
		} else {
			writeStoreError(w, "could not delete saved view")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
func (s *Server) favoriteApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	var input struct {
		Favorite bool `json:"favorite"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetApplicationFavorite(r.Context(), currentUser(r).ID, app.ID, input.Favorite); err != nil {
		writeStoreError(w, "could not update favorite")
		return
	}
	writeJSON(w, http.StatusOK, input)
}
func (s *Server) setApplicationLabels(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Labels map[string]string `json:"labels"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.ValidateApplicationLabels(input.Labels); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if app.RepositoryConfigurationID != "" || app.BranchTest != nil {
		writeError(w, http.StatusConflict, "labels are managed by Git; edit metadata.labels in justcd.yaml")
		return
	}
	if err := s.Store.SetApplicationLabels(r.Context(), app.ID, input.Labels); err != nil {
		writeError(w, http.StatusConflict, "could not update labels; the application may now be managed by Git")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.labels_updated", "application", app.ID, map[string]any{"labels": input.Labels})
	app, err = s.Store.ApplicationByID(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not load application")
		return
	}
	writeJSON(w, http.StatusOK, app)
}
