package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/syncer"
)

func (s *Server) getOwnershipConflict(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	conflicts, err := s.Syncer.OwnershipConflicts(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not inspect the current plan")
		return
	}
	var first *syncer.OwnershipConflict
	if len(conflicts) > 0 {
		first = conflicts[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{"conflict": first, "conflicts": conflicts})
}

type adoptBatchInput struct {
	Conflicts                  []syncer.OwnershipConflict `json:"conflicts"`
	Reason                     string                     `json:"reason"`
	PreviousControllerDisabled bool                       `json:"previousControllerDisabled"`
}

func decodeAdoptBatch(w http.ResponseWriter, r *http.Request) (adoptBatchInput, bool) {
	var input adoptBatchInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return input, false
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) > 500 || len(input.Conflicts) == 0 || len(input.Conflicts) > 200 {
		writeError(w, http.StatusBadRequest, "select between 1 and 200 resources and keep the optional reason to 500 characters")
		return input, false
	}
	if !input.PreviousControllerDisabled {
		writeError(w, http.StatusBadRequest, "confirm that the previous controller has stopped reconciling")
		return input, false
	}
	return input, true
}

// Reviewing and claiming many resources takes one agent round trip each, which
// outlasts gateway timeouts, so the work runs as a background task.
func (s *Server) adoptApplicationResources(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body is too large or unreadable")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if _, ok := decodeAdoptBatch(w, r); !ok {
		return
	}
	s.enqueueBackgroundTask(w, r, app.ID, "adopt", "", json.RawMessage(body))
}

func (s *Server) adoptApplicationResourcesNow(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	input, ok := decodeAdoptBatch(w, r)
	if !ok {
		return
	}
	results, err := s.Syncer.AdoptResources(r.Context(), app, currentUser(r).ID, input.Reason, input.Conflicts)
	if err != nil {
		if errors.Is(err, syncer.ErrAdoptionStale) || errors.Is(err, syncer.ErrAdoptionUnsafe) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			s.Logger.Warn("batch resource adoption failed", "applicationId", app.ID, "error", err)
			writeError(w, http.StatusUnprocessableEntity, "could not safely review the selected resources")
		}
		return
	}
	claimed := 0
	for index := range results {
		if results[index].Status == "claimed" {
			claimed++
		} else if results[index].Error != syncer.ErrAdoptionStale.Error() && results[index].Error != syncer.ErrAdoptionUnsafe.Error() {
			results[index].Error = "claim failed; refresh the conflict before retrying"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "claimed": claimed, "failed": len(results) - claimed, "autoSyncPaused": claimed > 0})
}

func (s *Server) adoptApplicationResource(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Conflict                   syncer.OwnershipConflict `json:"conflict"`
		Reason                     string                   `json:"reason"`
		PreviousControllerDisabled bool                     `json:"previousControllerDisabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "reason must be 500 characters or fewer")
		return
	}
	if !input.PreviousControllerDisabled {
		writeError(w, http.StatusBadRequest, "confirm that the previous controller has stopped reconciling")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	resource, err := s.Syncer.AdoptResource(ctx, app, currentUser(r).ID, input.Reason, input.Conflict)
	if err != nil {
		if errors.Is(err, syncer.ErrAdoptionStale) || errors.Is(err, syncer.ErrAdoptionUnsafe) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			s.Logger.Warn("resource adoption failed", "applicationId", app.ID, "error", err)
			writeStoreError(w, "could not finish recording the resource claim; refresh the conflict before retrying")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"identity": resource.Identity, "uid": resource.UID, "autoSyncPaused": true})
}
