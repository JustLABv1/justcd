package api

import (
	"context"
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/notifications"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) notificationRoutes() {
	base := "/api/v1/workspaces/{workspaceID}/notifications"
	s.Mux.Handle("GET "+base, s.requireAuth(http.HandlerFunc(s.listNotifications)))
	s.Mux.Handle("GET "+base+"/activity", s.requireAuth(http.HandlerFunc(s.notificationActivity)))
	s.Mux.Handle("POST "+base, s.requireAuth(s.requireCSRF(http.HandlerFunc(s.saveNotification))))
	s.Mux.Handle("PUT "+base+"/{destinationID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.saveNotification))))
	s.Mux.Handle("DELETE "+base+"/{destinationID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteNotification))))
	s.Mux.Handle("POST "+base+"/{destinationID}/test", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.testNotification))))
}
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	if !s.requireWorkspaceRole(w, r, r.PathValue("workspaceID"), "owner") {
		return
	}
	items, err := s.Store.NotificationDestinations(r.Context(), r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, "could not load notification destinations")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) saveNotification(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	var input struct {
		Name            string                `json:"name"`
		Provider        string                `json:"provider"`
		Enabled         bool                  `json:"enabled"`
		Events          []string              `json:"events"`
		MinimumSeverity string                `json:"minimumSeverity"`
		Config          *notifications.Config `json:"config"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 || notifications.Severity(input.MinimumSeverity) == 0 || len(input.Events) == 0 || len(input.Events) > 6 {
		writeError(w, 400, "name, selected events and minimum severity are required")
		return
	}
	for _, e := range input.Events {
		valid := false
		for _, t := range notifications.EventTypes[:6] {
			if e == t {
				valid = true
			}
		}
		if !valid {
			writeError(w, 400, "unsupported event")
			return
		}
	}
	id := r.PathValue("destinationID")
	if id != "" {
		items, err := s.Store.NotificationDestinations(r.Context(), workspace)
		if err != nil {
			writeStoreError(w, "could not load destination")
			return
		}
		found := false
		for _, d := range items {
			if d.ID == id {
				found = true
				if input.Config == nil && d.Provider != input.Provider {
					writeError(w, 400, "provider changes require new configuration")
					return
				}
			}
		}
		if !found {
			writeError(w, 404, "destination not found")
			return
		}
	} else {
		id = store.NewID()
		if input.Config == nil {
			writeError(w, 400, "destination configuration is required")
			return
		}
	}
	cipher := []byte{}
	if input.Config != nil {
		if err := input.Config.Validate(input.Provider); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if input.Provider != "email" {
			if _, err := notifications.Body(input.Provider, *input.Config, notifications.Event{SchemaVersion: 1, ID: "test", Type: "test", Severity: "info", URL: s.Config.FrontendURL}); err != nil {
				writeError(w, 400, "invalid JSON body template")
				return
			}
		}
		raw, _ := json.Marshal(input.Config)
		var err error
		cipher, err = security.Encrypt(s.EncryptionKey, raw, "notification:"+id)
		if err != nil {
			writeError(w, 500, "could not encrypt configuration")
			return
		}
	}
	d := store.NotificationDestination{ID: id, WorkspaceID: workspace, Name: input.Name, Provider: input.Provider, Enabled: input.Enabled, Events: input.Events, MinimumSeverity: input.MinimumSeverity, Cipher: cipher}
	if err := s.Store.SaveNotificationDestination(r.Context(), d, currentUser(r).ID); err != nil {
		writeStoreError(w, "could not save destination")
		return
	}
	writeJSON(w, 200, d)
}
func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	if err := s.Store.DeleteNotificationDestination(r.Context(), workspace, r.PathValue("destinationID"), currentUser(r).ID); err != nil {
		writeError(w, 404, "destination not found")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	id, err := s.Store.TestNotificationDestination(r.Context(), workspace, r.PathValue("destinationID"), currentUser(r).ID)
	if err != nil {
		writeError(w, 409, "destination unavailable; enable it before testing")
		return
	}
	writeJSON(w, 202, map[string]string{"eventId": id})
}
func (s *Server) notificationActivity(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	items, err := s.Store.NotificationActivity(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not load notification activity")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) RunNotificationWorker(ctx context.Context) {
	client := notifications.PublicClient()
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for i := 0; i < 20 && ctx.Err() == nil; i++ {
			attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			worked, err := s.Store.DeliverNotification(attemptCtx, func(d store.NotificationDestination, e notifications.Event, id string) notifications.Result {
				plain, err := security.Decrypt(s.EncryptionKey, d.Cipher, "notification:"+d.ID)
				if err != nil {
					return notifications.Result{Code: "configuration_unavailable"}
				}
				var c notifications.Config
				if json.Unmarshal(plain, &c) != nil {
					return notifications.Result{Code: "configuration_invalid"}
				}
				e.URL = strings.TrimRight(s.Config.FrontendURL, "/") + "/workspaces/" + url.PathEscape(e.WorkspaceID)
				if e.ApplicationID != "" {
					e.URL = strings.TrimRight(s.Config.FrontendURL, "/") + "/applications/" + url.PathEscape(e.ApplicationID)
				}
				return notifications.Deliver(attemptCtx, client, d.Provider, c, e, id)
			})
			cancel()
			if err != nil {
				s.Logger.WarnContext(ctx, "notification worker database failure")
				break
			}
			if !worked {
				break
			}
		}
	}
}
