package api

import (
	"context"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) sourceControlToken(ctx context.Context, connection store.SourceControlConnection) ([]byte, error) {
	if connection.StatusCredentialID == nil {
		return security.Decrypt(s.EncryptionKey, connection.StatusTokenCipher, "source-control-status:"+connection.ID)
	}
	return gitops.APIToken(ctx, s.Store, s.EncryptionKey, *connection.StatusCredentialID, connection.WorkspaceID)
}
