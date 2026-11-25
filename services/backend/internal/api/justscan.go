package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/justscan"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) justScanRoutes() {
	const admin = "/api/v1/admin/justscan"
	s.Mux.Handle("GET "+admin, s.requireAuth(s.requireAdmin(http.HandlerFunc(s.getJustScanServer))))
	s.Mux.Handle("PUT "+admin, s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.putJustScanServer)))))
	const connection = "/api/v1/workspaces/{workspaceID}/justscan"
	const scans = "/api/v1/plans/{planID}/justscan"
	s.Mux.Handle("GET "+connection, s.requireAuth(http.HandlerFunc(s.getJustScanConnection)))
	s.Mux.Handle("PUT "+connection, s.requireAuth(s.requireCSRF(http.HandlerFunc(s.putJustScanConnection))))
	s.Mux.Handle("DELETE "+connection, s.requireAuth(s.requireCSRF(http.HandlerFunc(s.deleteJustScanConnection))))
	s.Mux.Handle("GET "+scans, s.requireAuth(http.HandlerFunc(s.getPlanScans)))
	s.Mux.Handle("POST "+scans, s.requireAuth(s.requireCSRF(http.HandlerFunc(s.queuePlanScans))))
}

type justScanServerSettings struct {
	ServerURL string `json:"serverUrl"`
}

func (s *Server) getJustScanServer(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.Store.JustScanURL(r.Context())
	if err != nil {
		writeStoreError(w, "could not load JustScan server")
		return
	}
	writeJSON(w, http.StatusOK, justScanServerSettings{ServerURL: endpoint})
}

func (s *Server) putJustScanServer(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ServerURL *string `json:"serverUrl"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.ServerURL == nil {
		writeError(w, http.StatusBadRequest, "serverUrl is required")
		return
	}
	endpoint := strings.TrimSpace(*input.ServerURL)
	if endpoint != "" {
		parsed, err := justscan.ParseServer(endpoint)
		if err != nil || (s.Config.Production && parsed.Scheme != "https") {
			writeError(w, http.StatusBadRequest, "enter an HTTP(S) JustScan server URL. Production requires HTTPS.")
			return
		}
		endpoint = strings.TrimRight(parsed.String(), "/")
	}
	if err := s.Store.SetJustScanURL(r.Context(), endpoint, currentUser(r).ID); err != nil {
		writeStoreError(w, "could not update JustScan server")
		return
	}
	writeJSON(w, http.StatusOK, justScanServerSettings{ServerURL: endpoint})
}

type justScanSettings struct {
	ServerURL      string `json:"serverUrl"`
	OrganizationID string `json:"organizationId"`
	Connected      bool   `json:"connected"`
}

func (s *Server) getJustScanConnection(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	connection, err := s.Store.JustScanConnection(r.Context(), workspace)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not load JustScan connection")
		return
	}
	endpoint, err := s.Store.JustScanURL(r.Context())
	if err != nil {
		writeStoreError(w, "could not load JustScan server")
		return
	}
	writeJSON(w, http.StatusOK, justScanSettings{endpoint, connection.OrganizationID, endpoint != "" && connection.ServerURL == endpoint})
}

func (s *Server) putJustScanConnection(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	endpoint, err := s.Store.JustScanURL(r.Context())
	if err != nil {
		writeStoreError(w, "could not load JustScan server")
		return
	}
	if endpoint == "" {
		writeError(w, http.StatusConflict, "configure JustScan in the instance settings first")
		return
	}
	var input struct {
		ServerURL      string `json:"serverUrl"`
		OrganizationID string `json:"organizationId"`
		Token          string `json:"token"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JustScan connection")
		return
	}
	if input.ServerURL != endpoint {
		writeError(w, http.StatusConflict, store.ErrJustScanServerChanged.Error())
		return
	}
	input.OrganizationID = strings.ToLower(strings.TrimSpace(input.OrganizationID))
	input.Token = strings.TrimSpace(input.Token)
	if _, err := justscan.NewClient(endpoint, input.OrganizationID, input.Token); err != nil || len(input.Token) > 8192 || strings.ContainsAny(input.Token, "\r\n") {
		writeError(w, http.StatusBadRequest, "enter a JustScan organization UUID and pipeline token")
		return
	}
	cipher, err := security.Encrypt(s.EncryptionKey, []byte(input.Token), "justscan:"+workspace)
	if err != nil {
		writeStoreError(w, "could not encrypt JustScan token")
		return
	}
	if err := s.Store.SaveJustScanConnection(r.Context(), workspace, store.JustScanConnection{ServerURL: endpoint, OrganizationID: input.OrganizationID, TokenCipher: cipher}); err != nil {
		if errors.Is(err, store.ErrJustScanServerChanged) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreError(w, "could not save JustScan connection")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "justscan.connection_saved", "workspace", workspace, map[string]any{"organizationId": input.OrganizationID})
	writeJSON(w, http.StatusOK, justScanSettings{endpoint, input.OrganizationID, true})
}

func (s *Server) deleteJustScanConnection(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	if err := s.Store.DeleteJustScanConnection(r.Context(), workspace); err != nil {
		writeStoreError(w, "could not remove JustScan connection")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "justscan.connection_removed", "workspace", workspace, nil)
	w.WriteHeader(http.StatusNoContent)
}

type planScansResponse struct {
	Connected bool                     `json:"connected"`
	Items     []store.JustScanPlanScan `json:"items"`
}

func (s *Server) getPlanScans(w http.ResponseWriter, r *http.Request) {
	s.servePlanScans(w, r, "viewer", false)
}

func (s *Server) queuePlanScans(w http.ResponseWriter, r *http.Request) {
	s.servePlanScans(w, r, "deployer", true)
}

func (s *Server) servePlanScans(w http.ResponseWriter, r *http.Request, role string, queue bool) {
	record, app, ok := s.authorizedPlan(w, r, role)
	if !ok {
		return
	}
	response := planScansResponse{Items: []store.JustScanPlanScan{}}
	endpoint, err := s.Store.JustScanURL(r.Context())
	if err != nil {
		writeStoreError(w, "could not load JustScan server")
		return
	}
	if endpoint != "" {
		connection, err := s.Store.JustScanConnection(r.Context(), app.WorkspaceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeStoreError(w, "could not load JustScan connection")
			return
		}
		response.Connected = err == nil && connection.ServerURL == endpoint
	}
	if !response.Connected {
		if queue {
			writeError(w, http.StatusConflict, "connect JustScan in the workspace settings first")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	images, err := core.WorkloadImages(record.Desired)
	if err != nil {
		writeStoreError(w, "could not read plan image inventory")
		return
	}
	statusCode := http.StatusOK
	if queue {
		if record.Plan.Decommission || record.Status != "current" || !record.ExpiresAt.After(time.Now()) {
			writeError(w, http.StatusConflict, "only a current deployment plan can start scans")
			return
		}
		targets := make([]string, 0, len(images))
		for _, image := range images {
			if core.IsDigestPinnedImage(image) {
				targets = append(targets, image)
			}
		}
		if len(targets) > 0 {
			if err := s.Store.QueueJustScanPlanScans(r.Context(), app.WorkspaceID, record.ID, targets); err != nil {
				writeStoreError(w, "could not queue JustScan scans")
				return
			}
		}
		statusCode = http.StatusAccepted
	}
	scans, err := s.Store.JustScanPlanScans(r.Context(), record.ID)
	if err != nil {
		writeStoreError(w, "could not load JustScan scan statuses")
		return
	}
	byImage := make(map[string]store.JustScanPlanScan, len(scans))
	for _, scan := range scans {
		byImage[scan.Image] = scan
	}
	for _, image := range images {
		scan, ok := byImage[image]
		if !ok {
			scan = store.JustScanPlanScan{Image: image, Status: "unscanned"}
			if !core.IsDigestPinnedImage(image) {
				scan.Status = "unpinned"
			}
		}
		response.Items = append(response.Items, scan)
	}
	writeJSON(w, statusCode, response)
}
