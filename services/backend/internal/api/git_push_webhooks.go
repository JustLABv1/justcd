package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/security"
)

func (s *Server) getGitPushWebhook(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") || !s.requireWorkspaceGitSource(w, r, workspaceID, source.ID) {
		return
	}
	_, err = s.Store.GitPushWebhookSecret(r.Context(), source.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not load webhook settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": err == nil, "managedByOwner": source.WorkspaceID != workspaceID, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/git-sources/" + source.ID})
}

func (s *Server) putGitPushWebhook(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, source.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Secret string `json:"secret"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Secret) < 16 || strings.TrimSpace(input.Secret) != input.Secret {
		writeError(w, http.StatusBadRequest, "secret must be at least 16 characters without surrounding whitespace")
		return
	}
	cipher, err := security.Encrypt(s.EncryptionKey, []byte(input.Secret), "git-push-webhook:"+source.ID)
	if err != nil {
		writeStoreError(w, "could not encrypt webhook secret")
		return
	}
	if err := s.Store.SaveGitPushWebhook(r.Context(), source.ID, cipher); err != nil {
		writeStoreError(w, "could not save webhook settings")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "git_push_webhook.configured", "git_source", source.ID, map[string]any{"repositoryUrl": source.RepositoryURL})
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/git-sources/" + source.ID})
}

func (s *Server) genericGitPushWebhook(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	cipher, err := s.Store.GitPushWebhookSecret(r.Context(), source.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not configured")
		return
	}
	secret, err := security.Decrypt(s.EncryptionKey, cipher, "git-push-webhook:"+source.ID)
	if err != nil {
		writeStoreError(w, "could not verify webhook")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read webhook")
		return
	}
	provider := "generic"
	if r.Header.Get("X-GitHub-Event") != "" {
		provider = "github"
	}
	if r.Header.Get("X-Gitlab-Event") != "" {
		provider = "gitlab"
	}
	var event scm.Event
	if provider == "generic" {
		event, err = scm.VerifyGenericPush(string(secret), r.Header, body)
	} else {
		event, err = scm.VerifyAndParse(provider, string(secret), r.Header, body, time.Now())
	}
	if err != nil {
		if err.Error() == "event ignored" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if event.Kind != "push" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if provider != "generic" {
		host, repository, detailsErr := gitRepositoryParts(source.RepositoryURL)
		matches := detailsErr == nil && (repository == event.Repository || (provider == "gitlab" && strings.HasSuffix(repository, "/"+event.Repository)))
		if provider == "github" {
			matches = matches && host == "github.com"
		} else {
			matches = matches && host != "github.com"
		}
		if !matches {
			writeError(w, http.StatusForbidden, "webhook repository does not match Git source")
			return
		}
	}
	if event.DeliveryID == "" {
		sum := sha256.Sum256(body)
		event.DeliveryID = hex.EncodeToString(sum[:])
	}
	accepted, count, err := s.Store.RecordPushEvent(r.Context(), source.WorkspaceID, source.ID, "source:"+source.ID, provider, event.DeliveryID, event.Ref, event.HeadSHA)
	if err != nil {
		writeStoreError(w, "could not record Git push event")
		return
	}
	if accepted {
		_ = s.Store.Audit(r.Context(), "justcd-system", "webhook.push_received", "git_source", source.ID, map[string]any{"provider": provider, "deliveryId": event.DeliveryID, "repositoryUrl": source.RepositoryURL, "ref": event.Ref, "commit": event.HeadSHA, "matchedApplications": count})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted, "matchedApplications": count})
}
