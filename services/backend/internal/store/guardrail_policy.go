package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func (s *Store) GuardrailPolicy(ctx context.Context, workspaceID string) (core.PolicyConfig, error) {
	var raw []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT guardrail_policy FROM workspaces WHERE id=$1`, workspaceID).Scan(&raw); err != nil {
		return core.PolicyConfig{}, err
	}
	var config core.PolicyConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, err
	}
	return core.NormalizePolicyConfig(config)
}

// Lock affected applications before changing config so save/enqueue cannot
// commit a current plan using an old policy after invalidation has completed.
func (s *Store) SetGuardrailPolicy(ctx context.Context, workspaceID, actor string, config core.PolicyConfig) error {
	config, err := core.NormalizePolicyConfig(config)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspaceID).Scan(&locked); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM applications WHERE workspace_id=$1 ORDER BY id FOR UPDATE`, workspaceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		if err := rows.Scan(&locked); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var previous []byte
	if err := tx.QueryRowContext(ctx, `SELECT guardrail_policy FROM workspaces WHERE id=$1`, workspaceID).Scan(&previous); err != nil {
		return err
	}
	var prior core.PolicyConfig
	if err := json.Unmarshal(previous, &prior); err != nil {
		return err
	}
	before, err := core.PolicyConfigDigest(prior)
	if err != nil {
		return err
	}
	after, _ := core.PolicyConfigDigest(config)
	if before == after {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET guardrail_policy=$2,updated_at=NOW() WHERE id=$1`, workspaceID, raw); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE workspace_id=$1)`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'workspace.guardrail_policy_updated','workspace',$2,$3)`, actor, workspaceID, raw); err != nil {
		return err
	}
	return tx.Commit()
}

// Check current config and re-evaluate persisted desired manifests, even for
// legacy/already-approved plans. The evaluator has no database capabilities.
func checkPlanGuardrails(ctx context.Context, q policyQuery, planID string) error {
	return checkGuardrails(ctx, q, planID, true)
}

// Resource steps only need to verify the digest-bound evaluation and current
// config. Avoid loading/evaluating every desired manifest for every resource.
// Legacy plans still require evaluation because they have no saved findings.
func checkPlanGuardrailSnapshot(ctx context.Context, q policyQuery, planID string) error {
	return checkGuardrails(ctx, q, planID, false)
}

func checkGuardrails(ctx context.Context, q policyQuery, planID string, reevaluate bool) error {
	var configRaw, evaluationRaw, desiredRaw []byte
	var status string
	if err := q.QueryRowContext(ctx, `SELECT w.guardrail_policy,p.policy_evaluation,CASE WHEN $2 OR p.policy_evaluation='null'::jsonb THEN p.desired ELSE 'null'::jsonb END,p.status FROM plans p JOIN applications a ON a.id=p.application_id JOIN workspaces w ON w.id=a.workspace_id WHERE p.id=$1`, planID, reevaluate).Scan(&configRaw, &evaluationRaw, &desiredRaw, &status); err != nil {
		return err
	}
	if status != "current" {
		return errors.New("plan is no longer current; create a new plan")
	}
	var config core.PolicyConfig
	if err := json.Unmarshal(configRaw, &config); err != nil {
		return err
	}
	var evaluation *core.PolicyEvaluation
	if err := json.Unmarshal(evaluationRaw, &evaluation); err != nil {
		return err
	}
	digest, err := core.PolicyConfigDigest(config)
	if err != nil {
		return err
	}
	if evaluation != nil && evaluation.ConfigDigest != digest {
		return errors.New("project policy changed; create a new plan")
	}
	if err := evaluation.Authorize(); err != nil {
		return err
	}
	if !reevaluate && evaluation != nil {
		return nil
	}
	var desired []core.Resource
	if err := json.Unmarshal(desiredRaw, &desired); err != nil {
		return err
	}
	fresh, err := (core.BuiltinPolicyEngine{}).Evaluate(config, desired)
	if err != nil {
		return err
	}
	return fresh.Authorize()
}

func (s *Store) CheckPlanGuardrails(ctx context.Context, planID string) error {
	return checkPlanGuardrails(ctx, s.DB, planID)
}
