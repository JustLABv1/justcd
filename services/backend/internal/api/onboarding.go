package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
)

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

type onboardingResponse struct {
	Steps          []onboardingStep `json:"steps"`
	Completed      int              `json:"completed"`
	Total          int              `json:"total"`
	NextStepID     string           `json:"nextStepId,omitempty"`
	FirstWorkspaceID string           `json:"firstWorkspaceId,omitempty"`
}

type onboardingSnapshot struct {
	DatabaseReady, EncryptionAcknowledged, PublicURLReady   bool
	WorkspaceCount, KubernetesCredentialCount, ClusterCount   int
	NamespaceBindingCount, GitSourceCount, ApplicationCount int
	FirstWorkspaceID, ClusterStatus, ClusterCategory          string
	ClusterMessage, ClusterRemediation                      string
}

func (s *Server) onboardingStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := onboardingSnapshot{}
	if err := s.Store.DB.PingContext(r.Context()); err != nil {
		writeStoreError(w, "database readiness check failed")
		return
	}
	snapshot.DatabaseReady = true
	parsedPublicURL, err := url.Parse(s.Config.PublicURL)
	snapshot.PublicURLReady = err == nil && parsedPublicURL.Host != "" && (parsedPublicURL.Scheme == "https" || (parsedPublicURL.Scheme == "http" && (parsedPublicURL.Hostname() == "localhost" || parsedPublicURL.Hostname() == "127.0.0.1")))

	queries := []struct {
		destination *int
		query       string
	}{
		{&snapshot.WorkspaceCount, `SELECT COUNT(*) FROM workspaces`},
		{&snapshot.KubernetesCredentialCount, `SELECT COUNT(*) FROM credentials WHERE kind IN ('kubernetes-token','kubeconfig')`},
		{&snapshot.ClusterCount, `SELECT COUNT(*) FROM clusters`},
		{&snapshot.NamespaceBindingCount, `SELECT COUNT(*) FROM namespace_bindings`},
		{&snapshot.GitSourceCount, `SELECT COUNT(*) FROM git_sources`},
		{&snapshot.ApplicationCount, `SELECT COUNT(*) FROM applications`},
	}
	for _, item := range queries {
		if err := s.Store.DB.QueryRowContext(r.Context(), item.query).Scan(item.destination); err != nil {
			writeStoreError(w, "could not calculate onboarding readiness")
			return
		}
	}
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM workspaces ORDER BY created_at LIMIT 1`).Scan(&snapshot.FirstWorkspaceID)
	var acknowledged bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM onboarding_acknowledgements WHERE step_id='encryption-key')`).Scan(&acknowledged); err != nil {
		writeStoreError(w, "could not load onboarding progress")
		return
	}
	snapshot.EncryptionAcknowledged = acknowledged

	var rawReport []byte
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT report FROM kubernetes_permission_tests ORDER BY checked_at DESC LIMIT 1`).Scan(&rawReport)
	if err == nil {
		var report struct {
			Status  string                                           `json:"status"`
			Failure *struct{ Category, Message, Remediation string } `json:"failure"`
		}
		if json.Unmarshal(rawReport, &report) == nil {
			snapshot.ClusterStatus = report.Status
			if report.Failure != nil {
				snapshot.ClusterCategory = report.Failure.Category
				snapshot.ClusterMessage = report.Failure.Message
				snapshot.ClusterRemediation = report.Failure.Remediation
			}
		}
	} else if err != sql.ErrNoRows {
		writeStoreError(w, "could not load cluster readiness")
		return
	}
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
	if s.FirstWorkspaceID != "" {
		workspaceHref = "/workspaces/" + s.FirstWorkspaceID
	}
	connectionHref := clusterConnectionHref(s)
	steps := []onboardingStep{
		{ID: "database", Title: "Database ready", Status: status(s.DatabaseReady), Summary: choose(s.DatabaseReady, "PostgreSQL is reachable and migrations are available.", "PostgreSQL is not reachable."), Category: choose(s.DatabaseReady, "", "service_health"), Remediation: choose(s.DatabaseReady, "", "Check JUSTCD_DATABASE_URL, database availability, and migration permissions."), DocsHref: "https://github.com/JustLABv1/justcd/blob/main/charts/justcd/README.md#prerequisites"},
		{ID: "encryption-key", Title: "Encryption key persistence", Status: status(s.EncryptionAcknowledged), Summary: choose(s.EncryptionAcknowledged, "An administrator confirmed that the credential encryption key is stable and backed up.", "Confirm that JUSTCD_ENCRYPTION_KEY is stored durably before saving credentials."), Category: choose(s.EncryptionAcknowledged, "", "configuration"), Remediation: choose(s.EncryptionAcknowledged, "", "Back up the 32-byte key in your secret manager, then acknowledge this check."), DocsHref: "https://github.com/JustLABv1/justcd/blob/main/charts/justcd/README.md#prerequisites"},
		{ID: "public-url", Title: "Public URL", Status: status(s.PublicURLReady), Summary: choose(s.PublicURLReady, "The configured browser-facing URL is valid for this environment.", "The public URL is missing, invalid, or uses insecure HTTP outside localhost."), Category: choose(s.PublicURLReady, "", "configuration"), Remediation: choose(s.PublicURLReady, "", "Set JUSTCD_PUBLIC_URL to the HTTPS origin users open in their browser."), Href: "/settings", DocsHref: "https://github.com/JustLABv1/justcd/blob/main/charts/justcd/README.md#prerequisites"},
		{ID: "workspace", Title: "First workspace", Status: status(s.WorkspaceCount > 0), Summary: choose(s.WorkspaceCount > 0, "A delivery workspace is ready for connections and applications.", "Create a workspace to scope repositories, clusters, and access."), Category: choose(s.WorkspaceCount > 0, "", "configuration"), Href: workspaceHref},
		{ID: "cluster", Title: "Kubernetes access", Status: clusterStepStatus(s), Summary: clusterStepSummary(s), Category: clusterStepCategory(s), Remediation: clusterStepRemediation(s), Href: connectionHref, DocsHref: "https://github.com/JustLABv1/justcd#first-setup"},
		{ID: "git", Title: "Git access", Status: status(s.GitSourceCount > 0), Summary: choose(s.GitSourceCount > 0, "A Git source is connected. Revisit it at any time to run the safe connection test.", "Add repository credentials when needed, then connect a Git source."), Category: choose(s.GitSourceCount > 0, "", "configuration"), Remediation: choose(s.GitSourceCount > 0, "", "Add a Git source to the first workspace and test repository access."), Href: gitHref(s), DocsHref: "https://github.com/JustLABv1/justcd#first-setup"},
		{ID: "application", Title: "First application", Status: status(s.ApplicationCount > 0), Summary: choose(s.ApplicationCount > 0, "Your first application is configured.", "Create an application after Git and Kubernetes access are ready."), Category: choose(s.ApplicationCount > 0, "", "configuration"), Remediation: choose(s.ApplicationCount > 0, "", "Choose a source, renderer, cluster, and bound namespace."), Href: "/applications/new", DocsHref: "https://github.com/JustLABv1/justcd#first-setup"},
	}
	response := onboardingResponse{Steps: steps, Total: len(steps), FirstWorkspaceID: s.FirstWorkspaceID}
	for _, step := range steps {
		if step.Status == "complete" {
			response.Completed++
		} else if response.NextStepID == "" {
			response.NextStepID = step.ID
		}
	}
	return response
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
	if s.FirstWorkspaceID == "" {
		return "/workspaces/new"
	}
	return "/workspaces/" + s.FirstWorkspaceID + "/connections/git-sources"
}
func clusterConnectionHref(s onboardingSnapshot) string {
	if s.FirstWorkspaceID == "" {
		return "/workspaces/new"
	}
	return "/workspaces/" + s.FirstWorkspaceID + "/clusters/new"
}
func clusterStepStatus(s onboardingSnapshot) string {
	if s.WorkspaceCount == 0 || s.KubernetesCredentialCount == 0 || s.ClusterCount == 0 || s.NamespaceBindingCount == 0 || s.ClusterStatus == "" {
		return "action_required"
	}
	if s.ClusterStatus == "passed" {
		return "complete"
	}
	return "failed"
}
func clusterStepSummary(s onboardingSnapshot) string {
	switch {
	case s.WorkspaceCount == 0:
		return "Create a workspace before connecting a cluster."
	case s.KubernetesCredentialCount == 0:
		return "Add a Kubernetes token or kubeconfig; secret values remain backend-only."
	case s.ClusterCount == 0:
		return "Register the Kubernetes API endpoint and TLS details."
	case s.NamespaceBindingCount == 0:
		return "Bind a namespace to the workspace and choose its credential."
	case s.ClusterStatus == "":
		return "Run the safe discovery and permission test. It does not mutate cluster resources."
	case s.ClusterStatus == "passed":
		return "Kubernetes connectivity and required namespace permissions passed."
	case s.ClusterMessage != "":
		return s.ClusterMessage
	default:
		return "The latest Kubernetes readiness test needs attention."
	}
}
func clusterStepCategory(s onboardingSnapshot) string {
	if clusterStepStatus(s) == "complete" {
		return ""
	}
	if s.ClusterCategory != "" {
		switch s.ClusterCategory {
		case "authentication":
			return "configuration"
		case "authorization":
			return "permission"
		case "dependency", "network":
			return "connectivity"
		case "kubernetes":
			return "service_health"
		}
		return s.ClusterCategory
	}
	return "configuration"
}
func clusterStepRemediation(s onboardingSnapshot) string {
	if s.ClusterRemediation != "" {
		return s.ClusterRemediation
	}
	if clusterStepStatus(s) == "complete" {
		return ""
	}
	return "Complete the credential, endpoint, namespace binding, and permission-test workflow."
}
