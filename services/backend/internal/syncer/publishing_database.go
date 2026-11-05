package syncer

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Provision the bootstrap Secret only while applying the reviewed SGScript.
// Its values are encrypted in the credential store and never enter a plan or Git.
func (s *Service) ensurePublishingDatabase(ctx context.Context, input planInput, appID string, resource core.Resource) error {
	if resource.Identity.APIVersion != "stackgres.io/v1" || resource.Identity.Kind != "SGScript" {
		return nil
	}
	app, err := s.Store.ApplicationByID(ctx, appID)
	if err != nil {
		return err
	}
	source, err := s.Store.GitSourceByID(ctx, app.SourceID)
	if err != nil {
		return err
	}
	publication, err := s.Store.PublishedApplication(ctx, app.WorkspaceID, source.RepositoryURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var cfg publishing.Config
	if json.Unmarshal(publication.Config, &cfg) != nil {
		return errors.New("invalid publication configuration")
	}
	d, err := publishing.ManagedDatabase(cfg)
	if err != nil {
		return err
	}
	if d == nil {
		return nil
	}
	if resource.Identity.Name != publishing.ServiceResourceName(cfg.Name, "-db-init") || resource.Identity.Namespace != cfg.Namespace || resource.Identity.ClusterID != app.ClusterID {
		return nil
	}
	credential, err := s.Store.CredentialByID(ctx, d.CredentialID)
	if err != nil || credential.Kind != "publishing-database" || credential.WorkspaceID == nil || *credential.WorkspaceID != app.WorkspaceID {
		return errors.New("database credentials are unavailable")
	}
	plain, err := security.Decrypt(s.EncryptionKey, credential.Cipher, "credential:"+credential.ID)
	if err != nil {
		return errors.New("database credentials could not be decrypted")
	}
	var values publishing.DatabaseCredentials
	if json.Unmarshal(plain, &values) != nil {
		return errors.New("invalid protected database credentials")
	}
	client, err := clientFor(input, resource.Identity)
	if err != nil {
		return err
	}
	return ensureDatabaseSecret(ctx, client.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace(cfg.Namespace), cfg.Name, cfg.Namespace, appID, *d, credential.ID, values)
}
func ensureDatabaseSecret(ctx context.Context, secrets dynamic.ResourceInterface, appName, namespace, applicationID string, d publishing.DatabasePlan, credentialID string, values publishing.DatabaseCredentials) error {
	name := publishing.ExpandServiceReference(d.CredentialsSecret, appName, namespace)
	data := map[string]any{d.UsernameKey: base64.StdEncoding.EncodeToString([]byte(values.Username)), d.PasswordKey: base64.StdEncoding.EncodeToString([]byte(values.Password)), d.BootstrapKey: base64.StdEncoding.EncodeToString([]byte(values.SQL)), d.BootstrapKey + ".database": base64.StdEncoding.EncodeToString([]byte(values.DatabaseSQL))}
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.GetAnnotations()["justcd.io/database-credential"] != credentialID {
			return errors.New("database credential Secret already exists and is not owned by this publication")
		}
		actual, _, _ := unstructured.NestedStringMap(existing.Object, "data")
		for key, value := range data {
			if actual[key] != value {
				return errors.New("database credential Secret differs from protected values; restore it before retrying")
			}
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("could not inspect database credential Secret")
	}
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": map[string]any{"name": name, "namespace": namespace, "annotations": map[string]any{"justcd.io/retain": "true", "justcd.io/database-credential": credentialID}}, "data": data}}
	if _, err = secrets.Create(ctx, object, metav1.CreateOptions{FieldManager: "justcd/" + applicationID}); err != nil {
		return errors.New("could not create protected database credential Secret; check namespace permissions and retry")
	}
	return nil
}
