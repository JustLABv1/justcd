package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/resource"
)

var previewDNSName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}[a-z0-9]$`)

type sourceControlInput struct {
	Provider       string               `json:"provider"`
	APIURL         string               `json:"apiUrl"`
	Repository     string               `json:"repository,omitempty"`
	WebhookSecret  string               `json:"webhookSecret"`
	StatusToken    string               `json:"statusToken"`
	PreviewProfile store.PreviewProfile `json:"previewProfile"`
}

func validateSourceControlInput(input *sourceControlInput, source store.GitSource, app store.Application, previous *store.SourceControlConnection) error {
	input.Provider = strings.TrimSpace(input.Provider)
	input.APIURL = strings.TrimRight(strings.TrimSpace(input.APIURL), "/")
	details, err := sourceControlDetails(source, input.Provider, input.APIURL)
	if err != nil {
		return err
	}
	input.Provider, input.APIURL, input.Repository = details.Provider, details.APIURL, details.Repository
	if (previous == nil || input.WebhookSecret != "") && len(input.WebhookSecret) < 16 || (previous == nil || input.StatusToken != "") && len(input.StatusToken) < 16 {
		return errors.New("webhookSecret and statusToken must each have at least 16 characters")
	}
	if previous != nil && previous.Provider != input.Provider && (input.WebhookSecret == "" || input.StatusToken == "") {
		return errors.New("changing provider requires new webhookSecret and statusToken")
	}
	p := &input.PreviewProfile
	if !p.Enabled {
		return nil
	}
	if !previewDNSName.MatchString(p.NamespacePrefix) || p.MaxActive < 1 || p.MaxActive > 100 || p.MaxLifetimeHours < 1 || p.MaxLifetimeHours > 168 {
		return errors.New("preview profile needs a namespacePrefix, maxActive (1-100), and maxLifetimeHours (1-168)")
	}
	if p.QuotaCPU == "" || p.QuotaMemory == "" {
		return errors.New("preview profile needs CPU and memory quotas")
	}
	if _, err := resource.ParseQuantity(p.QuotaCPU); err != nil {
		return errors.New("quotaCpu must be a Kubernetes quantity")
	}
	if _, err := resource.ParseQuantity(p.QuotaMemory); err != nil {
		return errors.New("quotaMemory must be a Kubernetes quantity")
	}
	if p.DatabaseStrategy != "none" && p.DatabaseStrategy != "shared-preview" && p.DatabaseStrategy != "schema-per-pr" && p.DatabaseStrategy != "ephemeral" && p.DatabaseStrategy != "sanitized-snapshot" {
		return errors.New("databaseStrategy must name a preview-safe strategy")
	}
	if p.ManifestPath == "" && p.HelmValuesYAML == "" && len(p.HelmValuesFiles) == 0 {
		return errors.New("preview profile needs a preview manifest path or Helm values override")
	}
	if (p.HelmValuesYAML != "" || len(p.HelmValuesFiles) > 0) && app.Renderer != "helm" {
		return errors.New("preview Helm values require a Helm application")
	}
	if app.Renderer != "helm" && (p.ManifestPath == "" || p.ManifestPath == app.ManifestPath) {
		return errors.New("non-Helm previews need a separate preview overlay path")
	}
	if app.Renderer == "helm" {
		files, values, err := validateHelmValuesInput("helm", p.HelmValuesFiles, p.HelmValuesYAML)
		if err != nil {
			return err
		}
		p.HelmValuesFiles, p.HelmValuesYAML = files, values
	}
	if p.ManifestPath != "" {
		clean := path.Clean(p.ManifestPath)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(clean, '\x00') {
			return errors.New("preview manifestPath must be repository-relative")
		}
		p.ManifestPath = clean
	}
	if p.HostSuffix == "" || strings.ContainsAny(p.HostSuffix, "/:@ \\?") || !strings.Contains(p.HostSuffix, ".") {
		return errors.New("preview profile needs a valid hostSuffix for ingress validation")
	}
	for _, name := range p.AllowedSecrets {
		if !namespaceNamePattern.MatchString(name) {
			return errors.New("invalid allowed secret name")
		}
	}
	if p.AllowForks && len(p.AllowedSecrets) > 0 {
		return errors.New("fork previews cannot use allowed secrets")
	}
	return nil
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
	source, err := s.Store.GitSourceByID(r.Context(), app.SourceID)
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
		active, err := s.Store.PreviewSlotCount(r.Context(), previous.ID)
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
	} else {
		webhookCipher, err = security.Encrypt(s.EncryptionKey, []byte(input.WebhookSecret), "source-control-webhook:"+id)
		if err != nil {
			writeStoreError(w, "could not encrypt webhook secret")
			return
		}
	}
	if input.StatusToken == "" && previous != nil {
		statusCipher = previous.StatusTokenCipher
	} else {
		statusCipher, err = security.Encrypt(s.EncryptionKey, []byte(input.StatusToken), "source-control-status:"+id)
		if err != nil {
			writeStoreError(w, "could not encrypt status token")
			return
		}
	}
	c := store.SourceControlConnection{ID: id, WorkspaceID: app.WorkspaceID, ApplicationID: app.ID, Provider: input.Provider, APIURL: input.APIURL, Repository: input.Repository, WebhookSecretCipher: webhookCipher, StatusTokenCipher: statusCipher, PreviewProfile: input.PreviewProfile}
	if err := s.Store.SaveSourceControlConnection(r.Context(), c); err != nil {
		writeStoreError(w, "could not save source control connection")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "source_control.configured", "application", app.ID, map[string]any{"provider": c.Provider, "repository": c.Repository, "previewEnabled": c.PreviewProfile.Enabled})
	writeJSON(w, http.StatusOK, map[string]any{"source": sourceControlSource{RepositoryURL: source.RepositoryURL, Provider: c.Provider, APIURL: c.APIURL, Repository: c.Repository, SelfHosted: c.Provider == "gitlab" && c.APIURL != "https://gitlab.com/api/v4"}, "connection": c, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/source-control/" + c.ID})
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
	source, err := s.Store.GitSourceByID(r.Context(), app.SourceID)
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
		writeJSON(w, http.StatusOK, map[string]any{"source": suggestion, "connection": nil, "webhookUrl": ""})
		return
	}
	if err != nil {
		writeStoreError(w, "could not load source control connection")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": suggestion, "connection": c, "webhookUrl": s.Config.PublicURL + "/api/v1/webhooks/source-control/" + c.ID})
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
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) sourceControlWebhook(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.SourceControlConnectionByID(r.Context(), r.PathValue("connectionID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "source control connection not found")
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
		source, err := s.Store.GitSourceByID(r.Context(), app.SourceID)
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
