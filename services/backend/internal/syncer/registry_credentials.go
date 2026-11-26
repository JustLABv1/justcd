package syncer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// rendererUsesHelm reports whether a render can download Helm charts.
func rendererUsesHelm(app store.Application) bool {
	return app.Renderer == "helm" || (app.Renderer == "kustomize" && app.KustomizeHelmEnabled)
}

// registryCredentials decrypts the helm-registry credentials available to a
// workspace's renders.
func (s *Service) registryCredentials(ctx context.Context, workspaceID string) ([]render.RegistryAuth, error) {
	credentials, err := s.Store.HelmRegistryCredentials(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load Helm registry credentials: %w", err)
	}
	auths := make([]render.RegistryAuth, 0, len(credentials))
	for _, credential := range credentials {
		auth, err := decryptRegistryCredential(s.EncryptionKey, credential)
		if err != nil {
			return nil, err
		}
		auths = append(auths, auth)
	}
	return auths, nil
}

func decryptRegistryCredential(key []byte, credential store.Credential) (render.RegistryAuth, error) {
	plain, err := security.Decrypt(key, credential.Cipher, "credential:"+credential.ID)
	if err != nil {
		return render.RegistryAuth{}, fmt.Errorf("could not decrypt Helm registry credential %q", credential.Name)
	}
	var secret struct {
		Username string `json:"username"`
		Password string `json:"password"`
		CAData   string `json:"caData"`
	}
	if err := json.Unmarshal(plain, &secret); err != nil {
		return render.RegistryAuth{}, fmt.Errorf("Helm registry credential %q is invalid", credential.Name)
	}
	return render.RegistryAuth{Endpoint: credential.Endpoint, Username: secret.Username, Password: secret.Password, CAData: secret.CAData}, nil
}
