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
		s.Logger.ErrorContext(r.Context(), "could not load plans for application topology", "applicationId", app.ID, "error", err)
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
	if !hasSample && (r.URL.Query().Get("refresh") == "1" || (r.URL.Query().Get("cached") != "1" && stale)) {
		if err := s.Syncer.RefreshApplicationHealth(r.Context(), app); err != nil {
			s.Logger.Warn("topology observation incomplete", "applicationId", app.ID, "error", err)
			warning = "Live descendants could not be fully observed. Check namespace read permissions or cluster connectivity."
		}
		if refreshed, err := s.Store.ObservedResources(r.Context(), app.ID); err == nil {
			observed = refreshed
		}
		if refreshed, err := s.Store.ApplicationByID(r.Context(), app.ID); err == nil {
			app = refreshed
		}
	}
	options := topology.BuildOptions{ObservationIncomplete: warning != "" || len(app.HealthCondition.Warnings) > 0}
	if !hasSample {
		for _, item := range observed {
			if item.Source == "kubernetes" && time.Since(item.ObservedAt) > time.Minute {
				options.ObservationIncomplete = true
			}
		}
	}
	var graph topology.Graph
	if len(plans) > 0 {
		graph = topology.BuildWithOptions(&plans[0], managed, observed, options)
	} else {
		graph = topology.BuildWithOptions(nil, managed, observed, options)
	}
	if warning != "" {
		graph.Warnings = append(graph.Warnings, warning)
	}
	if len(managed) == 0 && !hasSample {
		graph.Warnings = append(graph.Warnings, "No managed workloads to observe. Desired resources and adoption conflicts do not include live pods; review and claim the intended existing resources, or deploy this application first.")
	}
	writeJSON(w, http.StatusOK, graph)
}
