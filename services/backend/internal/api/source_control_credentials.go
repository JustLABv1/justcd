package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) sourceControlToken(ctx context.Context, connection store.SourceControlConnection) ([]byte, error) {
	if connection.StatusCredentialID == nil {
		return security.Decrypt(s.EncryptionKey, connection.StatusTokenCipher, "source-control-status:"+connection.ID)
	}
	credential, err := s.Store.CredentialByID(ctx, *connection.StatusCredentialID)
	if err != nil || credential.Kind != "git-https" || (credential.WorkspaceID != nil && *credential.WorkspaceID != connection.WorkspaceID) {
		return nil, errors.New("choose an accessible Git HTTPS token credential")
	}
	if credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now()) {
		return nil, errors.New("Git credential has expired")
	}
	plain, err := security.Decrypt(s.EncryptionKey, credential.Cipher, "credential:"+credential.ID)
	if err != nil {
		return nil, err
	}
	var secret gitops.Credentials
	if json.Unmarshal(plain, &secret) != nil || len(secret.Token) < 16 {
		return nil, errors.New("Git credential needs an API-capable token of at least 16 characters")
	}
	return []byte(secret.Token), nil
}
