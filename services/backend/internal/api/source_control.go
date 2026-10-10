package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type sourceControlInput struct {
	PipelineStatusReporting bool                 `json:"pipelineStatusReporting"`
	StatusCredentialID      string               `json:"statusCredentialId"`
	Provider                string               `json:"provider"`
	APIURL                  string               `json:"apiUrl"`
	Repository              string               `json:"repository,omitempty"`
	WebhookSecret           string               `json:"webhookSecret"`
	StatusToken             string               `json:"statusToken"`
	PreviewProfile          store.PreviewProfile `json:"previewProfile"`
}

func validateSourceControlInput(input *sourceControlInput, source store.GitSource, app store.Application, previous *store.SourceControlConnection) error {
	input.Provider = strings.TrimSpace(input.Provider)
	input.APIURL = strings.TrimRight(strings.TrimSpace(input.APIURL), "/")
	details, err := sourceControlDetails(source, input.Provider, input.APIURL)
	if err != nil {
		return err
	}
	input.Provider, input.APIURL, input.Repository = details.Provider, details.APIURL, details.Repository
	if input.WebhookSecret != "" && len(input.WebhookSecret) < 16 || input.StatusCredentialID == "" && (previous == nil || input.StatusToken != "") && len(input.StatusToken) < 16 {
		return errors.New("webhookSecret, when supplied, and statusToken must each have at least 16 characters")
	}
	if previous != nil && previous.Provider != input.Provider && input.StatusToken == "" && input.StatusCredentialID == "" {
		return errors.New("changing provider requires a new statusToken")
	}
	return store.ValidatePreviewProfile(&input.PreviewProfile, app)
}

func sourceMatchesConnection(sourceURL, provider string, apiURL *url.URL, repository string) bool {
	details, err := sourceControlDetails(store.GitSource{RepositoryURL: sourceURL}, provider, apiURL.String())
	return err == nil && details.Repository == repository
}

func (s *Server) putSourceControl(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if connection, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID); err == nil && connection.ManagedByGit {
		writeError(w, http.StatusConflict, "PR handling is managed by justcd.yaml; edit spec.pullRequests in Git")
		return
	}
	source, err := s.Store.GitSourceForWorkspace(r.Context(), app.SourceID, app.WorkspaceID)
	if err != nil {
		writeStoreError(w, "Git source unavailable")
		return
	}
	var input sourceControlInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := store.NewID()
	var previous *store.SourceControlConnection
	if old, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID); err == nil {
		id = old.ID
		previous = &old
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not load connection")
		return
	}
	if err := validateSourceControlInput(&input, source, app, previous); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if previous != nil {
		active, err := s.Store.ActivePreviewCount(r.Context(), previous.ID)
		if err != nil {
			writeStoreError(w, "could not check active previews")
			return
		}
		if active > 0 && (previous.Provider != input.Provider || previous.APIURL != input.APIURL || previous.Repository != input.Repository || !samePreviewProfile(previous.PreviewProfile, input.PreviewProfile)) {
			writeError(w, http.StatusConflict, "close existing previews before changing the provider or preview profile")
			return
		}
	}
	var webhookCipher, statusCipher []byte
	if input.WebhookSecret == "" && previous != nil {
		webhookCipher = previous.WebhookSecretCipher
	} else if input.WebhookSecret != "" {
		webhookCipher, err = security.Encrypt(s.EncryptionKey, []byte(input.WebhookSecret), "source-control-webhook:"+id)
		if err != nil {
			writeStoreError(w, "could not encrypt webhook secret")
			return
		}
	}
	var statusCredentialID *string
	if input.StatusCredentialID != "" {
		if input.StatusToken != "" {
			writeError(w, http.StatusBadRequest, "choose a saved credential or a new API token")
			return
		}
		candidate := store.SourceControlConnection{WorkspaceID: app.WorkspaceID, StatusCredentialID: &input.StatusCredentialID}
		if _, err := s.sourceControlToken(r.Context(), candidate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		statusCredentialID = &input.StatusCredentialID
		statusCipher = []byte{}
	} else if input.StatusToken == "" && previous != nil {
		statusCredentialID = previous.StatusCredentialID
		statusCipher = previous.StatusTokenCipher
	} else {
		statusCipher, err = security.Encrypt(s.EncryptionKey, []byte(input.StatusToken), "source-control-status:"+id)
		if err != nil {
			writeStoreError(w, "could not encrypt status token")
			return
		}
	}
	enabled := previous == nil || previous.Enabled
	c := store.SourceControlConnection{PipelineStatusReporting: input.PipelineStatusReporting, ID: id, Enabled: enabled, WorkspaceID: app.WorkspaceID, ApplicationID: app.ID, Provider: input.Provider, APIURL: input.APIURL, Repository: input.Repository, WebhookSecretCipher: webhookCipher, StatusTokenCipher: statusCipher, StatusCredentialID: statusCredentialID, PreviewProfile: input.PreviewProfile}
	if err := s.Store.SaveSourceControlConnection(r.Context(), c); err != nil {
		if errors.Is(err, store.ErrGitManagedPR) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreError(w, "could not save source control connection")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "source_control.configured", "application", app.ID, map[string]any{"provider": c.Provider, "repository": c.Repository, "previewEnabled": c.PreviewProfile.Enabled})
	writeJSON(w, http.StatusOK, map[string]any{"source": sourceControlSource{RepositoryURL: source.RepositoryURL, Provider: c.Provider, APIURL: c.APIURL, Repository: c.Repository, SelfHosted: c.Provider == "gitlab" && c.APIURL != "https://gitlab.com/api/v4"}, "connection": c, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/source-control/" + c.ID, "webhookConfigured": len(c.WebhookSecretCipher) > 0})
}

func samePreviewProfile(a, b store.PreviewProfile) bool {
	if a.HelmValuesFiles == nil {
		a.HelmValuesFiles = []string{}
	}
	if b.HelmValuesFiles == nil {
		b.HelmValuesFiles = []string{}
	}
	if a.AllowedSecrets == nil {
		a.AllowedSecrets = []string{}
	}
	if b.AllowedSecrets == nil {
		b.AllowedSecrets = []string{}
	}
	return reflect.DeepEqual(a, b)
}

func (s *Server) getSourceControl(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	source, err := s.Store.GitSourceForWorkspace(r.Context(), app.SourceID, app.WorkspaceID)
	if err != nil {
		writeStoreError(w, "Git source unavailable")
		return
	}
	suggestion, err := sourceControlSuggestion(source)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	c, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"source": suggestion, "connection": nil, "webhookUrl": "", "webhookConfigured": false})
		return
	}
	if err != nil {
		writeStoreError(w, "could not load source control connection")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": suggestion, "connection": c, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/source-control/" + c.ID, "webhookConfigured": len(c.WebhookSecretCipher) > 0})
}

func (s *Server) removeSourceControlWebhook(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if err := s.Store.RemoveSourceControlWebhookSecret(r.Context(), app.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR reporting is not configured")
			return
		}
		writeStoreError(w, "could not remove webhook secret")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "source_control.webhook_removed", "application", app.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setSourceControlEnabled(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if connection, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID); err == nil && connection.ManagedByGit {
		writeError(w, http.StatusConflict, "PR handling is managed by justcd.yaml; edit spec.pullRequests in Git")
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if err := s.Store.SetSourceControlConnectionEnabled(r.Context(), app.ID, *input.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR reporting is not configured")
			return
		}
		if errors.Is(err, store.ErrActivePreviews) || errors.Is(err, store.ErrGitManagedPR) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreError(w, "could not change PR reporting state")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "source_control.enabled_changed", "application", app.ID, map[string]any{"enabled": *input.Enabled})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSourceControl(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if connection, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID); err == nil && connection.ManagedByGit {
		writeError(w, http.StatusConflict, "PR handling is managed by justcd.yaml; edit spec.pullRequests in Git")
		return
	}
	if err := s.Store.DeleteSourceControlConnection(r.Context(), app.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR reporting is not configured")
			return
		}
		if errors.Is(err, store.ErrActivePreviews) || errors.Is(err, store.ErrGitManagedPR) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreError(w, "could not delete PR reporting")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "source_control.deleted", "application", app.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPullRequestReviews(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	c, err := s.Store.SourceControlConnectionByApplication(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "source control not configured")
		return
	}
	items, err := s.Store.ListReviews(r.Context(), c.ID)
	if err != nil {
		writeStoreError(w, "could not load pull request reviews")
		return
	}
	for i := range items {
		if items[i].Phase == "adoption_available" {
			items[i].BranchPreviews, err = s.Store.MatchingBranchPreviews(r.Context(), app, items[i])
			if err != nil {
				writeStoreError(w, "could not load matching branch previews")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) sourceControlWebhook(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.SourceControlConnectionByID(r.Context(), r.PathValue("connectionID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "source control connection not found")
		return
	}
	if !c.Enabled {
		writeError(w, http.StatusGone, "PR reporting is disabled")
		return
	}
	if len(c.WebhookSecretCipher) == 0 {
		writeError(w, http.StatusGone, "webhook is not configured; pull requests are polled")
		return
	}
	secret, err := security.Decrypt(s.EncryptionKey, c.WebhookSecretCipher, "source-control-webhook:"+c.ID)
	if err != nil {
		writeStoreError(w, "could not verify webhook")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read webhook")
		return
	}
	event, err := scm.VerifyAndParse(c.Provider, string(secret), r.Header, body, time.Now())
	if err != nil {
		if err.Error() == "event ignored" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if event.Repository != c.Repository {
		writeError(w, http.StatusForbidden, "webhook repository does not match connection")
		return
	}
	// A stable digest also deduplicates legacy GitLab events without a delivery UUID.
	if event.DeliveryID == "" {
		sum := sha256.Sum256(body)
		event.DeliveryID = hex.EncodeToString(sum[:])
	}
	if event.Kind == "push" {
		app, err := s.Store.ApplicationByID(r.Context(), c.ApplicationID)
		if err != nil {
			writeStoreError(w, "could not load webhook application")
			return
		}
		source, err := s.Store.GitSourceForWorkspace(r.Context(), app.SourceID, app.WorkspaceID)
		apiURL, parseErr := url.Parse(c.APIURL)
		if err != nil || parseErr != nil || !sourceMatchesConnection(source.RepositoryURL, c.Provider, apiURL, c.Repository) {
			writeError(w, http.StatusConflict, "application Git source no longer matches source control connection")
			return
		}
		accepted, count, err := s.Store.RecordPushEvent(r.Context(), c.WorkspaceID, app.SourceID, "connection:"+c.ID, c.Provider, event.DeliveryID, event.Ref, event.HeadSHA)
		if err != nil {
			writeStoreError(w, "could not record Git push event")
			return
		}
		if accepted {
			_ = s.Store.Audit(r.Context(), "justcd-system", "webhook.push_received", "application", c.ApplicationID, map[string]any{"provider": c.Provider, "deliveryId": event.DeliveryID, "repository": event.Repository, "ref": event.Ref, "commit": event.HeadSHA, "matchedApplications": count})
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted, "matchedApplications": count})
		return
	}
	v := store.PullRequestReview{ID: store.NewID(), ConnectionID: c.ID, Number: event.Number, HeadSHA: event.HeadSHA, SourceURL: event.URL, Fork: event.Fork, Closed: event.Closed, EventAt: event.EventAt}
	accepted, err := s.Store.RecordReviewEvent(r.Context(), c.ID, event.DeliveryID, v)
	if err != nil {
		writeStoreError(w, "could not record pull request event")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted})
}
