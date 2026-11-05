package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// APIToken decrypts the provider API token held by an HTTPS Git credential
// that is available to the workspace.
func APIToken(ctx context.Context, st *store.Store, key []byte, credentialID, workspaceID string) ([]byte, error) {
	credential, err := st.CredentialByID(ctx, credentialID)
	if err != nil || credential.Kind != "git-https" || (credential.WorkspaceID != nil && *credential.WorkspaceID != workspaceID) {
		return nil, errors.New("choose an accessible Git HTTPS token credential")
	}
	if credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now()) {
		return nil, errors.New("Git credential has expired")
	}
	plain, err := security.Decrypt(key, credential.Cipher, "credential:"+credential.ID)
	if err != nil {
		return nil, err
	}
	var secret Credentials
	if json.Unmarshal(plain, &secret) != nil || len(secret.Token) < 16 {
		return nil, errors.New("Git credential needs an API-capable token of at least 16 characters")
	}
	return []byte(secret.Token), nil
}
