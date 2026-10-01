package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/security"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var agentProfilePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (s *Server) agentOwner(w http.ResponseWriter, r *http.Request) bool {
	c, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil {
		writeError(w, 404, "cluster not found")
		return false
	}
	if c.WorkspaceID == nil {
		if !currentUser(r).IsAdmin {
			writeError(w, 403, "instance administrator required")
			return false
		}
		return true
	}
	return s.requireWorkspaceRole(w, r, *c.WorkspaceID, "owner")
}
func (s *Server) getClusterAgent(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspace, "viewer") || !s.requireWorkspaceCluster(w, r, workspace, r.PathValue("clusterID")) {
		return
	}
	a, err := s.Store.ClusterAgent(r.Context(), r.PathValue("clusterID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"mode": "direct"})
		return
	}
	if err != nil {
		writeStoreError(w, "could not load agent")
		return
	}
	items, summary, err := s.Store.ListAgentActivity(r.Context(), a.ClusterID, workspace)
	if err != nil {
		writeStoreError(w, "could not load agent activity")
		return
	}
	// Profiles are reported by the cluster operator; do not disclose other tenants.
	profiles := make([]agentprotocol.Profile, 0)
	for _, profile := range a.Profiles {
		for _, id := range profile.WorkspaceIDs {
			if id == workspace {
				profile.WorkspaceIDs = []string{workspace}
				profile.TokenFile = ""
				profiles = append(profiles, profile)
				break
			}
		}
	}
	a.Profiles = profiles
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"activity": items, "summary": summary, "mode": "agent", "online": !a.Revoked && a.LastSeenAt != nil && time.Since(*a.LastSeenAt) < time.Minute, "agent": a})
}
func (s *Server) enrollClusterAgent(w http.ResponseWriter, r *http.Request) {
	if !s.agentOwner(w, r) {
		return
	}
	var in struct {
		DefaultProfile string `json:"defaultProfile"`
		ClusterProfile string `json:"clusterProfile"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.DefaultProfile == "" {
		in.DefaultProfile = "default"
	}
	if !agentProfilePattern.MatchString(in.DefaultProfile) || (in.ClusterProfile != "" && !agentProfilePattern.MatchString(in.ClusterProfile)) {
		writeError(w, 400, "invalid agent profile name")
		return
	}
	token, hash, err := security.RandomToken(32)
	if err != nil {
		writeStoreError(w, "could not create enrollment token")
		return
	}
	if err = s.Store.ConfigureClusterAgent(r.Context(), r.PathValue("clusterID"), in.DefaultProfile, in.ClusterProfile, hash); err != nil {
		writeError(w, http.StatusConflict, "could not configure agent; wait for queued or running cluster operations, then try again")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.agent_enrollment", "cluster", r.PathValue("clusterID"), nil)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"enrollmentToken": token, "expiresAt": time.Now().Add(10 * time.Minute), "clusterId": r.PathValue("clusterID")})
}
func (s *Server) revokeClusterAgent(w http.ResponseWriter, r *http.Request) {
	if !s.agentOwner(w, r) {
		return
	}
	direct := r.URL.Query().Get("mode") == "direct"
	if err := s.Store.RevokeClusterAgent(r.Context(), r.PathValue("clusterID"), direct); err != nil {
		writeStoreError(w, "could not revoke agent")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "cluster.agent_revoked", "cluster", r.PathValue("clusterID"), map[string]any{"direct": direct})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (s *Server) agentEnroll(w http.ResponseWriter, r *http.Request) {
	var e agentprotocol.Enrollment
	if err := decodeJSON(w, r, &e); err != nil {
		writeError(w, 400, "invalid enrollment payload")
		return
	}
	if e.Protocol != agentprotocol.Version || e.ClusterUID == "" || len(e.ClusterUID) > 128 || len(e.Version) > 128 || len(e.Profiles) == 0 || len(e.Profiles) > 32 {
		writeError(w, 400, "unsupported agent protocol or invalid identity")
		return
	}
	for i := range e.Profiles {
		p := &e.Profiles[i]
		p.TokenFile = ""
		if !agentProfilePattern.MatchString(p.Name) || len(p.WorkspaceIDs) > 100 || len(p.Namespaces) > 100 {
			writeError(w, 400, "invalid agent profile")
			return
		}
	}
	token, hash, err := security.RandomToken(32)
	if err != nil {
		writeStoreError(w, "could not issue identity")
		return
	}
	id, err := s.Store.EnrollClusterAgent(r.Context(), security.HashToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), hash, e)
	if err != nil {
		writeError(w, 401, "enrollment is expired, consumed, revoked, or belongs to a different cluster")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, agentprotocol.Identity{ClusterUID: e.ClusterUID, Token: token, ClusterID: id, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
}
func (s *Server) authenticatedAgent(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		writeError(w, 401, "agent identity required")
		return "", false
	}
	id, err := s.Store.AuthenticateClusterAgent(r.Context(), security.HashToken(token))
	if err != nil {
		writeError(w, 401, "agent identity expired or revoked")
		return "", false
	}
	return id, true
}
func (s *Server) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authenticatedAgent(w, r)
	if !ok {
		return
	}
	if err := s.Store.AgentHeartbeat(r.Context(), id); err != nil {
		writeStoreError(w, "heartbeat failed")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) agentTasks(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authenticatedAgent(w, r)
	if !ok {
		return
	}
	lease, _, err := security.RandomToken(32)
	if err != nil {
		writeStoreError(w, "could not issue lease")
		return
	}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok = s.authenticatedAgent(w, r); !ok {
			return
		}
		taskID, cipher, err := s.Store.ClaimAgentTask(r.Context(), id, lease)
		if err == nil {
			plain, err := security.Decrypt(s.EncryptionKey, cipher, "agent-task:"+taskID+":request")
			if err != nil {
				writeStoreError(w, "could not decode task")
				return
			}
			var task agentprotocol.Request
			if err = json.Unmarshal(plain, &task); err != nil {
				writeStoreError(w, "invalid task")
				return
			}
			task.Lease = lease
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, 200, task)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			writeStoreError(w, "could not fetch task")
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			w.WriteHeader(204)
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) agentResult(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authenticatedAgent(w, r)
	if !ok {
		return
	}
	var result agentprotocol.Result
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil || len(result.Body) > agentprotocol.MaxBody || len(result.ErrorCode) > 64 || len(result.Error) > 1024 || len(result.ContentType) > 256 || result.Status < 0 || result.Status > 599 || (result.Error == "" && result.Status < 100) {
		writeError(w, 400, "invalid task result")
		return
	}
	plain, err := json.Marshal(result)
	if err != nil {
		writeError(w, 400, "invalid result")
		return
	}
	cipher, err := security.Encrypt(s.EncryptionKey, plain, "agent-task:"+r.PathValue("taskID")+":response")
	if err != nil {
		writeStoreError(w, "could not store result")
		return
	}
	if err = s.Store.CompleteAgentTask(r.Context(), id, r.PathValue("taskID"), result.Lease, cipher, result); err != nil {
		writeError(w, 410, "task lease expired or already completed")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) agentRenew(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authenticatedAgent(w, r)
	if !ok {
		return
	}
	token, hash, err := security.RandomToken(32)
	if err != nil {
		writeStoreError(w, "could not renew identity")
		return
	}
	if err = s.Store.RenewClusterAgent(r.Context(), id, security.HashToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), hash); err != nil {
		writeError(w, 401, "identity cannot be renewed")
		return
	}
	a, err := s.Store.ClusterAgent(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not load identity")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, agentprotocol.Identity{ClusterUID: a.ClusterUID, Token: token, ClusterID: id, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
}
