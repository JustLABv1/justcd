package syncer

import (
	"context"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Service) evaluateGuardrails(ctx context.Context, app store.Application, plan *core.Plan, desired []core.Resource) error {
	config, err := s.Store.GuardrailPolicy(ctx, app.WorkspaceID)
	if err != nil {
		return err
	}
	evaluation, err := (core.BuiltinPolicyEngine{}).Evaluate(config, desired)
	if err != nil {
		return err
	}
	plan.PolicyEvaluation = &evaluation
	return nil
}
