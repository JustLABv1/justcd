package api

import (
	"net/http"
	"time"

	"github.com/justlab/justcd/services/backend/internal/topology"
)

func (s *Server) applicationTopology(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	plans, err := s.Store.ListPlans(r.Context(), app.ID, 1)
	if err != nil {
		writeStoreError(w, "could not load application plan")
		return
	}
	managed, err := s.Store.ManagedResources(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not load managed resources")
		return
	}
	observed, err := s.Store.ObservedResources(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not load observed resources")
		return
	}
	warning := ""
	hasSample := false
	for _, resource := range observed {
		if resource.Source == "sample" {
			hasSample = true
			break
		}
	}
	if !hasSample && len(managed) == 0 && len(observed) > 0 {
		if err := s.Store.ReplaceObservedResources(r.Context(), app.ID, nil); err != nil {
			writeStoreError(w, "could not clear stale resource observations")
			return
		}
		observed = nil
	}
	stale := len(observed) == 0
	for _, resource := range observed {
		if resource.Source == "kubernetes" && time.Since(resource.ObservedAt) > 30*time.Second {
			stale = true
			break
		}
	}
	if !hasSample && (r.URL.Query().Get("refresh") == "1" || (r.URL.Query().Get("cached") != "1" && stale)) && len(managed) > 0 {
		if err := s.Syncer.RefreshApplicationHealth(r.Context(), app); err != nil {
			s.Logger.Warn("topology observation incomplete", "applicationId", app.ID, "error", err)
			warning = "Live descendants could not be fully observed. Check namespace read permissions or cluster connectivity."
		}
		if refreshed, err := s.Store.ObservedResources(r.Context(), app.ID); err == nil {
			observed = refreshed
		}
	}
	var graph topology.Graph
	if len(plans) > 0 {
		graph = topology.Build(&plans[0], managed, observed)
	} else {
		graph = topology.Build(nil, managed, observed)
	}
	if warning != "" {
		graph.Warnings = append(graph.Warnings, warning)
	}
	writeJSON(w, http.StatusOK, graph)
}
