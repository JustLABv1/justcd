package api

import (
	"context"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) cleanupManagedApplicationNamespaces(ctx context.Context, app store.Application) error {
	plans, err := s.Store.ListPlans(ctx, app.ID, 1)
	if err != nil {
		return err
	}
	preserve := len(plans) > 0 && plans[0].Plan.Decommission && plans[0].Plan.Selection.KeepNamespaces
	return kube.CleanupApplicationNamespaces(ctx, s.Store, s.EncryptionKey, app, preserve)
}
