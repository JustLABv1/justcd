package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const onboardingDocs = "https://github.com/JustLABv1/justcd#first-setup"
const onboardingPrereqDocs = "https://github.com/JustLABv1/justcd/blob/main/charts/justcd/README.md#prerequisites"

// agentOnlineWindow is how recently an agent must have checked in to count as connected.
const agentOnlineWindow = 2 * time.Minute

type onboardingStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Category    string `json:"category,omitempty"`
	Summary     string `json:"summary"`
	Remediation string `json:"remediation,omitempty"`
	Href        string `json:"href,omitempty"`
	DocsHref    string `json:"docsHref,omitempty"`
}

// onboardingCluster is one cluster the workspace can deploy to, with a connection state
// that is meaningful for both agent and direct connections.
type onboardingCluster struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Mode       string     `json:"mode"`
	State      string     `json:"state"`
	Message    string     `json:"message,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

type onboardingResponse struct {
	// Steps are the workspace-scoped goals that make up the guided setup.
	Steps      []onboardingStep `json:"steps"`
	Completed  int              `json:"completed"`
	Total      int              `json:"total"`
	NextStepID string           `json:"nextStepId,omitempty"`
	// WorkspaceID is the workspace the progress was computed for.
	WorkspaceID      string              `json:"workspaceId,omitempty"`
	FirstWorkspaceID string              `json:"firstWorkspaceId,omitempty"`
	Clusters         []onboardingCluster `json:"clusters"`
	// System are instance-wide operator checks; they never block the guided steps.
	System          []onboardingStep `json:"system"`
	SystemAttention int              `json:"systemAttention"`
}

type onboardingSnapshot struct {
	DatabaseReady, EncryptionAcknowledged, PublicURLReady bool
	WorkspaceID                                           string
	WorkspaceCount, GitSourceCount, ApplicationCount      int
	Clusters                                              []onboardingCluster
}

// clusterConnectionState reduces the stored facts about one cluster to a user-facing state.
// Agent clusters are judged by their heartbeat; direct clusters by their latest permission test.
func clusterConnectionState(agent, revoked, enrollmentPending bool, lastSeen *time.Time, permissionStatus, permissionMessage string, now time.Time) (string, string) {
	if agent {
		switch {
		case revoked:
			return "revoked", "The agent was revoked. Create a new enrollment token to reconnect it."
		case lastSeen != nil && now.Sub(*lastSeen) < agentOnlineWindow:
			return "online", "The agent is connected and checking in."
		case lastSeen != nil:
			return "offline", "The agent has not checked in recently. Check that its pod is running and can reach JustCD."
		case enrollmentPending:
			return "waiting", "Waiting for the agent to enroll with the one-use token."
		default:
			return "not_enrolled", "Create an enrollment token and install the agent in the cluster."
		}
	}
	switch permissionStatus {
	case "passed":
		return "verified", "Connectivity and the required permissions were verified."
	case "partial":
		if permissionMessage != "" {
			return "partial", permissionMessage
		}
		return "partial", "Connected, but some permission checks did not pass. Deployments may be limited."
	case "":
		return "unverified", "Run the access check to verify connectivity and permissions."
	default:
		if permissionMessage != "" {
			return "failed", permissionMessage
		}
		return "failed", "The latest access check needs attention."
	}
}

// clusterReady reports whether JustCD can use the cluster; a partly verified direct connection still works.
func clusterReady(state string) bool {
	return state == "online" || state == "verified" || state == "partial"
}

func (s *Server) onboardingStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := onboardingSnapshot{}
	if err := s.Store.DB.PingContext(r.Context()); err != nil {
		writeStoreError(w, "database readiness check failed")
		return
	}
	snapshot.DatabaseReady = true
	parsedPublicURL, err := url.Parse(s.Config.PublicURL)
	snapshot.PublicURLReady = err == nil && parsedPublicURL.Host != "" && (parsedPublicURL.Scheme == "https" || (parsedPublicURL.Scheme == "http" && (parsedPublicURL.Hostname() == "localhost" || parsedPublicURL.Hostname() == "127.0.0.1" || parsedPublicURL.Hostname() == "::1")))

	workspaces, err := s.Store.ListWorkspaces(r.Context(), currentUser(r))
	if err != nil {
		writeStoreError(w, "could not load workspaces")
		return
	}
	snapshot.WorkspaceCount = len(workspaces)
	workspaceID := r.URL.Query().Get("workspaceId")
	if workspaceID != "" {
		if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
			return
		}
	} else if len(workspaces) > 0 {
		workspaceID = workspaces[0].ID
	}
	snapshot.WorkspaceID = workspaceID

	if workspaceID != "" {
		for _, item := range []struct {
			destination *int
			query       string
		}{
			{&snapshot.GitSourceCount, `SELECT COUNT(*) FROM git_sources WHERE workspace_id=$1`},
			{&snapshot.ApplicationCount, `SELECT COUNT(*) FROM applications WHERE workspace_id=$1`},
		} {
			if err := s.Store.DB.QueryRowContext(r.Context(), item.query, workspaceID).Scan(item.destination); err != nil {
				writeStoreError(w, "could not calculate onboarding readiness")
				return
			}
		}
		rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT c.id,c.name,
			EXISTS(SELECT 1 FROM cluster_agents a WHERE a.cluster_id=c.id),
			COALESCE((SELECT a.revoked FROM cluster_agents a WHERE a.cluster_id=c.id),FALSE),
			(SELECT a.last_seen_at FROM cluster_agents a WHERE a.cluster_id=c.id),
			COALESCE((SELECT a.enrollment_hash IS NOT NULL AND COALESCE(a.enrollment_expires_at,NOW()-INTERVAL '1 second')>NOW() FROM cluster_agents a WHERE a.cluster_id=c.id),FALSE),
			(SELECT t.report FROM kubernetes_permission_tests t WHERE t.cluster_id=c.id AND t.workspace_id=$1 ORDER BY t.checked_at DESC LIMIT 1)
			FROM clusters c
			WHERE c.workspace_id=$1 OR c.id IN (SELECT cluster_id FROM workspace_cluster_shares WHERE target_workspace_id=$1 AND status='accepted')
			ORDER BY c.created_at`, workspaceID)
		if err != nil {
			writeStoreError(w, "could not load cluster readiness")
			return
		}
		defer rows.Close()
		now := time.Now()
		for rows.Next() {
			var item onboardingCluster
			var agent, revoked, pending bool
			var lastSeen sql.NullTime
			var rawReport []byte
			if err := rows.Scan(&item.ID, &item.Name, &agent, &revoked, &lastSeen, &pending, &rawReport); err != nil {
				writeStoreError(w, "could not load cluster readiness")
				return
			}
			var seen *time.Time
			if lastSeen.Valid {
				seen = &lastSeen.Time
				item.LastSeenAt = seen
			}
			var report struct {
				Status  string                    `json:"status"`
				Failure *struct{ Message string } `json:"failure"`
			}
			if len(rawReport) > 0 {
				_ = json.Unmarshal(rawReport, &report)
			}
			message := ""
			if report.Failure != nil {
				message = report.Failure.Message
			}
			item.Mode = "direct"
			if agent {
				item.Mode = "agent"
			}
			item.State, item.Message = clusterConnectionState(agent, revoked, pending, seen, report.Status, message, now)
			snapshot.Clusters = append(snapshot.Clusters, item)
		}
		if err := rows.Err(); err != nil {
			writeStoreError(w, "could not load cluster readiness")
			return
		}
	}
	var acknowledged bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM onboarding_acknowledgements WHERE step_id='encryption-key')`).Scan(&acknowledged); err != nil {
		writeStoreError(w, "could not load onboarding progress")
		return
	}
	snapshot.EncryptionAcknowledged = acknowledged
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, buildOnboardingResponse(snapshot))
}

func (s *Server) completeOnboardingStep(w http.ResponseWriter, r *http.Request) {
	stepID := r.PathValue("stepID")
	if stepID != "encryption-key" {
		writeError(w, http.StatusBadRequest, "this onboarding step is derived from system state and cannot be completed manually")
		return
	}
	_, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO onboarding_acknowledgements(step_id,completed_by) VALUES($1,$2) ON CONFLICT(step_id) DO UPDATE SET completed_by=EXCLUDED.completed_by,completed_at=NOW()`, stepID, currentUser(r).ID)
	if err != nil {
		writeStoreError(w, "could not save onboarding progress")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

func buildOnboardingResponse(s onboardingSnapshot) onboardingResponse {
	workspaceHref := "/workspaces/new"
	if s.WorkspaceID != "" {
		workspaceHref = "/workspaces/" + s.WorkspaceID
	}
	system := []onboardingStep{
		{ID: "database", Title: "Database ready", Status: status(s.DatabaseReady), Summary: choose(s.DatabaseReady, "PostgreSQL is reachable and migrations are available.", "PostgreSQL is not reachable."), Category: choose(s.DatabaseReady, "", "service_health"), Remediation: choose(s.DatabaseReady, "", "Check JUSTCD_DATABASE_URL, database availability, and migration permissions."), DocsHref: onboardingPrereqDocs},
		{ID: "encryption-key", Title: "Encryption key persistence", Status: status(s.EncryptionAcknowledged), Summary: choose(s.EncryptionAcknowledged, "An administrator confirmed that the credential encryption key is stable and backed up.", "Confirm that JUSTCD_ENCRYPTION_KEY is stored durably before saving credentials."), Category: choose(s.EncryptionAcknowledged, "", "configuration"), Remediation: choose(s.EncryptionAcknowledged, "", "Back up the 32-byte key in your secret manager, then acknowledge this check."), DocsHref: onboardingPrereqDocs},
		{ID: "public-url", Title: "Public URL", Status: status(s.PublicURLReady), Summary: choose(s.PublicURLReady, "The configured browser-facing URL is valid for this environment.", "The public URL is missing, invalid, or uses insecure HTTP outside localhost."), Category: choose(s.PublicURLReady, "", "configuration"), Remediation: choose(s.PublicURLReady, "", "Set JUSTCD_PUBLIC_URL to the HTTPS origin users open in their browser."), Href: "/settings", DocsHref: onboardingPrereqDocs},
	}
	clusterStatus, clusterSummary, clusterRemediation := clusterStep(s)
	steps := []onboardingStep{
		{ID: "workspace", Title: "Create a workspace", Status: status(s.WorkspaceCount > 0), Summary: choose(s.WorkspaceCount > 0, "A delivery workspace is ready for connections and applications.", "Create a workspace to scope repositories, clusters, and access."), Category: choose(s.WorkspaceCount > 0, "", "configuration"), Href: workspaceHref},
		{ID: "cluster", Title: "Connect a cluster", Status: clusterStatus, Summary: clusterSummary, Remediation: clusterRemediation, Category: choose(clusterStatus == "complete", "", choose(clusterStatus == "failed", "connectivity", "configuration")), Href: clusterConnectionHref(s), DocsHref: onboardingDocs},
		{ID: "git", Title: "Connect Git", Status: status(s.GitSourceCount > 0), Summary: choose(s.GitSourceCount > 0, "A Git source is connected. Revisit it at any time to run the safe connection test.", "Add repository credentials when needed, then connect a Git source."), Category: choose(s.GitSourceCount > 0, "", "configuration"), Remediation: choose(s.GitSourceCount > 0, "", "Add a Git source to this workspace and test repository access."), Href: gitHref(s), DocsHref: onboardingDocs},
		{ID: "application", Title: "Deploy your first application", Status: status(s.ApplicationCount > 0), Summary: choose(s.ApplicationCount > 0, "Your first application is configured.", "Create an application once a cluster and a Git source are connected."), Category: choose(s.ApplicationCount > 0, "", "configuration"), Remediation: choose(s.ApplicationCount > 0, "", "Choose a source, renderer, cluster, and namespace."), Href: "/applications/new", DocsHref: onboardingDocs},
	}
	response := onboardingResponse{Steps: steps, Total: len(steps), WorkspaceID: s.WorkspaceID, FirstWorkspaceID: s.WorkspaceID, Clusters: s.Clusters, System: system}
	if response.Clusters == nil {
		response.Clusters = []onboardingCluster{}
	}
	for _, step := range steps {
		if step.Status == "complete" {
			response.Completed++
		} else if response.NextStepID == "" {
			response.NextStepID = step.ID
		}
	}
	for _, step := range system {
		if step.Status != "complete" {
			response.SystemAttention++
		}
	}
	return response
}

// clusterStep summarises the cluster goal. Any ready cluster, agent or direct, completes it.
func clusterStep(s onboardingSnapshot) (string, string, string) {
	if s.WorkspaceCount == 0 || s.WorkspaceID == "" {
		return "action_required", "Create a workspace before connecting a cluster.", "Create a workspace first."
	}
	var ready []string
	var attention *onboardingCluster
	for index := range s.Clusters {
		cluster := &s.Clusters[index]
		if clusterReady(cluster.State) {
			ready = append(ready, cluster.Name)
		} else if attention == nil {
			attention = cluster
		}
	}
	switch {
	case len(ready) == 1:
		return "complete", ready[0] + " is connected.", ""
	case len(ready) > 1:
		return "complete", strconv.Itoa(len(ready)) + " clusters are connected.", ""
	case attention != nil && attention.State == "failed":
		return "failed", attention.Name + ": " + attention.Message, "Fix the reported problem, then run the access check again."
	case attention != nil:
		return "action_required", attention.Name + ": " + attention.Message, "Finish connecting this cluster, or add another one."
	default:
		return "action_required", "Install the JustCD agent in a cluster, or connect its Kubernetes API directly.", "The agent connects outbound, so the cluster does not need to be reachable from JustCD."
	}
}

func status(ok bool) string {
	if ok {
		return "complete"
	}
	return "action_required"
}
func choose(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
func gitHref(s onboardingSnapshot) string {
	if s.WorkspaceID == "" {
		return "/workspaces/new"
	}
	return "/workspaces/" + s.WorkspaceID + "/connections/git-sources"
}
func clusterConnectionHref(s onboardingSnapshot) string {
	if s.WorkspaceID == "" {
		return "/workspaces/new"
	}
	return "/workspaces/" + s.WorkspaceID + "/clusters/new"
}
