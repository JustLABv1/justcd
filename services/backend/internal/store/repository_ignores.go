package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrGitManagedIgnore = errors.New("ignore rule is managed by Git; edit spec.ignoreResources in justcd.yaml")

func repositoryIgnoreID(appID, identity string) string {
	sum := sha256.Sum256([]byte(appID + "\x00" + identity))
	return "git-" + hex.EncodeToString(sum[:16])
}

// Existing UI rules remain in place; only Git-owned rows are reconciled.
func replaceRepositoryIgnores(ctx context.Context, tx *sql.Tx, app Application) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_ignore_rules WHERE application_id=$1 AND managed_by_git`, app.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_ignore_selectors WHERE application_id=$1 AND managed_by_git`, app.ID); err != nil {
		return err
	}
	for _, rule := range app.RepositoryIgnoreRules {
		id := repositoryIgnoreID(app.ID, "resource:"+rule.Identity.Key())
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_ignore_rules(id,application_id,cluster_id,api_version,kind,namespace,name,path,reason,created_by,managed_by_git) VALUES($1,$2,$3,$4,$5,$6,$7,'',$8,'justcd-system',TRUE)`, id, app.ID, rule.Identity.ClusterID, rule.Identity.APIVersion, rule.Identity.Kind, rule.Identity.Namespace, rule.Identity.Name, rule.Reason); err != nil {
			return fmt.Errorf("could not reconcile Git ignore resource; remove any identical manual rule first: %w", err)
		}
	}
	for _, rule := range app.RepositoryIgnoreSelectors {
		id := repositoryIgnoreID(app.ID, "selector:"+rule.APIVersion+"\x00"+rule.Kind+"\x00"+rule.LabelKey+"\x00"+rule.LabelValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_ignore_selectors(id,application_id,api_version,kind,label_key,label_value,reason,created_by,managed_by_git) VALUES($1,$2,$3,$4,$5,$6,$7,'justcd-system',TRUE)`, id, app.ID, rule.APIVersion, rule.Kind, rule.LabelKey, rule.LabelValue, rule.Reason); err != nil {
			return fmt.Errorf("could not reconcile Git ignore selector; remove any identical manual rule first: %w", err)
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, app.ID)
	return err
}
