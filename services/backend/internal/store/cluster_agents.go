package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
)

type ClusterAgent struct {
	ClusterID      string                  `json:"clusterId"`
	DefaultProfile string                  `json:"defaultProfile"`
	ClusterProfile string                  `json:"clusterProfile"`
	ClusterUID     string                  `json:"clusterUid"`
	Version        string                  `json:"version"`
	Profiles       []agentprotocol.Profile `json:"profiles"`
	LastSeenAt     *time.Time              `json:"lastSeenAt,omitempty"`
	Revoked        bool                    `json:"revoked"`
}

func (s *Store) ClusterAgent(ctx context.Context, id string) (ClusterAgent, error) {
	var a ClusterAgent
	var profiles []byte
	err := s.DB.QueryRowContext(ctx, `SELECT cluster_id,default_profile,cluster_profile,cluster_uid,version,profiles,last_seen_at,revoked FROM cluster_agents WHERE cluster_id=$1`, id).Scan(&a.ClusterID, &a.DefaultProfile, &a.ClusterProfile, &a.ClusterUID, &a.Version, &profiles, &a.LastSeenAt, &a.Revoked)
	if err == nil {
		err = json.Unmarshal(profiles, &a.Profiles)
	}
	return a, err
}
func (s *Store) ConfigureClusterAgent(ctx context.Context, id, defaultProfile, clusterProfile string, hash []byte) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize against operation claims through their cluster gate.
	var gate string
	if err = tx.QueryRowContext(ctx, `SELECT cluster_id FROM cluster_operation_gates WHERE cluster_id=$1 FOR UPDATE`, id).Scan(&gate); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations o JOIN applications a ON a.id=o.application_id WHERE a.cluster_id=$1 AND o.status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("wait for queued or running cluster operations before changing the agent connection")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM cluster_agent_tasks WHERE cluster_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cluster_agents(cluster_id,default_profile,cluster_profile,enrollment_hash,enrollment_expires_at,revoked) VALUES($1,$2,$3,$4,NOW()+INTERVAL '10 minutes',FALSE) ON CONFLICT(cluster_id) DO UPDATE SET default_profile=$2,cluster_profile=$3,enrollment_hash=$4,enrollment_expires_at=NOW()+INTERVAL '10 minutes',token_hash=NULL,previous_token_hash=NULL,token_expires_at=NULL,last_seen_at=NULL,revoked=FALSE`, id, defaultProfile, clusterProfile, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE cluster_id=$1)`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) EnrollClusterAgent(ctx context.Context, hash, tokenHash []byte, e agentprotocol.Enrollment) (string, error) {
	profiles, _ := json.Marshal(e.Profiles)
	var id string
	err := s.DB.QueryRowContext(ctx, `UPDATE cluster_agents SET enrollment_hash=NULL,enrollment_expires_at=NULL,token_hash=$2,token_expires_at=NOW()+INTERVAL '30 days',cluster_uid=$3,version=$4,profiles=$5,last_seen_at=NOW() WHERE enrollment_hash=$1 AND enrollment_expires_at>NOW() AND NOT revoked AND (cluster_uid='' OR cluster_uid=$3) RETURNING cluster_id`, hash, tokenHash, e.ClusterUID, e.Version, profiles).Scan(&id)
	return id, err
}
func (s *Store) AuthenticateClusterAgent(ctx context.Context, hash []byte) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT cluster_id FROM cluster_agents WHERE (token_hash=$1 OR (previous_token_hash=$1 AND previous_token_expires_at>NOW())) AND token_expires_at>NOW() AND NOT revoked`, hash).Scan(&id)
	return id, err
}
func (s *Store) AgentHeartbeat(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE cluster_agents SET last_seen_at=NOW() WHERE cluster_id=$1 AND NOT revoked`, id)
	return err
}
func (s *Store) RevokeClusterAgent(ctx context.Context, id string, direct bool) error {
	if direct {
		return s.updateConnectionAndInvalidate(ctx, `DELETE FROM cluster_agents WHERE cluster_id=$1`, id)
	}
	return s.updateConnectionAndInvalidate(ctx, `UPDATE cluster_agents SET revoked=TRUE,token_hash=NULL,previous_token_hash=NULL,enrollment_hash=NULL,last_seen_at=NULL WHERE cluster_id=$1`, id)
}
func (s *Store) QueueAgentTask(ctx context.Context, id, clusterID string, cipher []byte, deadline time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var agentID string
	err = tx.QueryRowContext(ctx, `SELECT cluster_id FROM cluster_agents WHERE cluster_id=$1 AND NOT revoked AND token_expires_at>NOW() AND last_seen_at>NOW()-INTERVAL '60 seconds' FOR UPDATE`, clusterID).Scan(&agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("cluster agent is offline; reconnect the agent and refresh the plan")
	}
	if err != nil {
		return err
	}
	var queued int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_agent_tasks WHERE cluster_id=$1 AND deadline>NOW()`, clusterID).Scan(&queued); err != nil {
		return err
	}
	if queued >= 500 {
		return errors.New("cluster agent task queue is full")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cluster_agent_tasks(id,cluster_id,request_cipher,deadline) VALUES($1,$2,$3,$4)`, id, clusterID, cipher, deadline); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ClaimAgentTask(ctx context.Context, clusterID, lease string) (string, []byte, error) {
	var id string
	var cipher []byte
	err := s.DB.QueryRowContext(ctx, `WITH next AS(SELECT id FROM cluster_agent_tasks WHERE cluster_id=$1 AND state='queued' AND deadline>NOW() ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE cluster_agent_tasks SET state='claimed',lease=$2 WHERE id=(SELECT id FROM next) RETURNING id,request_cipher`, clusterID, lease).Scan(&id, &cipher)
	return id, cipher, err
}
func (s *Store) CompleteAgentTask(ctx context.Context, clusterID, id, lease string, cipher []byte) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE cluster_agent_tasks SET state='completed',response_cipher=$4 WHERE cluster_id=$1 AND id=$2 AND lease=$3 AND state='claimed' AND deadline>NOW()`, clusterID, id, lease, cipher)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) AgentTaskResult(ctx context.Context, id string) ([]byte, error) {
	var cipher []byte
	err := s.DB.QueryRowContext(ctx, `SELECT response_cipher FROM cluster_agent_tasks WHERE id=$1 AND state='completed'`, id).Scan(&cipher)
	return cipher, err
}
func (s *Store) DeleteAgentTask(ctx context.Context, id string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM cluster_agent_tasks WHERE id=$1 OR deadline<NOW()-INTERVAL '5 minutes'`, id)
}

func (s *Store) RenewClusterAgent(ctx context.Context, id string, oldHash, newHash []byte) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE cluster_agents SET previous_token_hash=CASE WHEN token_hash=$2 THEN token_hash ELSE previous_token_hash END,previous_token_expires_at=NOW()+INTERVAL '5 minutes',token_hash=$3,token_expires_at=NOW()+INTERVAL '30 days' WHERE cluster_id=$1 AND (token_hash=$2 OR (previous_token_hash=$2 AND previous_token_expires_at>NOW())) AND token_expires_at>NOW() AND NOT revoked`, id, oldHash, newHash)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// Expired tasks are removed, never requeued, including after a server restart.
func (s *Store) PruneAgentTasks(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cluster_agent_tasks WHERE deadline<NOW()-INTERVAL '5 minutes'`)
	return err
}
