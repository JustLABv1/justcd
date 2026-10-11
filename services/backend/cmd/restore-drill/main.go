// restore-drill operates only on the disposable database created by scripts/restore-drill.sh.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("JUSTCD_RESTORE_DRILL") != "disposable" {
		return fmt.Errorf("requires JUSTCD_RESTORE_DRILL=disposable; never run against an existing database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	key, err := security.ParseEncryptionKey(os.Getenv("JUSTCD_ENCRYPTION_KEY"))
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, os.Getenv("JUSTCD_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.DB.Close()
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: restore-drill seed|verify|wrong-key")
	}
	if os.Args[1] == "seed" {
		var count int
		if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM workspaces`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("refusing to seed nonempty database")
		}
		tx, err := db.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		queries := []string{
			`INSERT INTO users(id,email,display_name,password_hash,is_admin) VALUES('dr-user','dr@example.invalid','Restore drill','not-a-login-hash',TRUE)`,
			`INSERT INTO workspaces(id,name) VALUES('dr-workspace','Restore drill')`,
			`INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('dr-source','dr-workspace','Restore drill','https://example.invalid/dr.git')`,
			`INSERT INTO clusters(id,name,api_server) VALUES('dr-cluster','Restore drill','https://example.invalid')`,
			`INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,sync_policy,auto_sync_paused) VALUES('dr-app','dr-workspace','Restore drill','dr-source','dr-revision','manifests','yaml','dr-cluster','manual',TRUE)`,
			`INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at,status) VALUES('dr-plan','dr-app','dr-revision','dr-digest','[]','[]','dr-user',NOW()+INTERVAL '1 hour','applied')`,
			`INSERT INTO operations(id,application_id,plan_id,actor_id,status,message,progress,finished_at) VALUES('dr-operation','dr-app','dr-plan','dr-user','succeeded','Synthetic restored operation','{"phase":"complete","total":2,"completed":[{"apiVersion":"v1","kind":"ConfigMap","namespace":"justcd-target","name":"first"},{"apiVersion":"v1","kind":"ConfigMap","namespace":"justcd-target","name":"second"}]}',NOW())`,
			`INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at,status) VALUES('dr-pending-plan','dr-app','dr-revision','pending-digest','[]','[]','dr-user',NOW()+INTERVAL '1 hour','current')`,
			`INSERT INTO operations(id,application_id,plan_id,actor_id,status,progress) VALUES('dr-queued','dr-app','dr-pending-plan','dr-user','queued','{"phase":"queued","total":2,"completed":[]}'),('dr-running','dr-app','dr-pending-plan','dr-user','running','{"phase":"apply","total":2,"completed":[{"apiVersion":"v1","kind":"ConfigMap","namespace":"justcd-target","name":"first"}]}')`,
			`INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES('dr-app','dr-running',NOW()+INTERVAL '1 hour')`,
			`INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES('dr-user','drill.completed','operation','dr-operation','{"synthetic":true}')`,
		}
		for _, query := range queries {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return err
			}
		}
		for _, kind := range []string{"git-https", "kubernetes-token"} {
			id := "dr-" + kind
			cipher, err := security.Encrypt(key, credentialPayload(kind), "credential:"+id)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,'dr-workspace',$1,$2,$3)`, id, kind, cipher); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else if os.Args[1] != "verify" && os.Args[1] != "wrong-key" {
		return fmt.Errorf("unknown mode")
	}
	for _, kind := range []string{"git-https", "kubernetes-token"} {
		id := "dr-" + kind
		c, err := db.CredentialByID(ctx, id)
		if err != nil {
			return err
		}
		plain, err := security.Decrypt(key, c.Cipher, "credential:"+id)
		if os.Args[1] == "wrong-key" {
			if err == nil {
				return fmt.Errorf("incorrect key unexpectedly decrypted credential")
			}
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(plain, credentialPayload(kind)) {
			return fmt.Errorf("credential contents differ")
		}
	}
	if os.Args[1] == "wrong-key" {
		fmt.Println("Incorrect key rejected for both credential kinds")
		return nil
	}
	// Hash exact persisted rows, including ciphertext, timestamps, IDs, plan payload,
	// operation progress, and audit details. Compare before/after dump and restore.
	plan, err := db.PlanByID(ctx, "dr-plan")
	if err != nil {
		return err
	}
	if plan.Status != "applied" || plan.Plan.Digest != "dr-digest" {
		return fmt.Errorf("plan model differs")
	}
	operation, err := db.OperationByID(ctx, "dr-operation")
	if err != nil {
		return err
	}
	if operation.Status != "succeeded" || len(operation.Progress.Completed) != 2 || operation.Progress.Phase != "complete" {
		return fmt.Errorf("operation model differs")
	}
	partial, err := db.OperationByID(ctx, "dr-running")
	if err != nil {
		return err
	}
	if len(partial.Progress.Completed) != 1 {
		return fmt.Errorf("partial progress differs")
	}
	var content string
	if err := db.DB.QueryRowContext(ctx, `SELECT jsonb_build_object('credentials',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM credentials c),'plans',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM plans p),'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM operations o),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_events a))::text`).Scan(&content); err != nil {
		return err
	}
	var valid bool
	if err := db.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plans WHERE id='dr-plan' AND digest='dr-digest' AND status='applied') AND EXISTS(SELECT 1 FROM operations WHERE id='dr-operation' AND status='succeeded' AND jsonb_array_length(progress->'completed')=2) AND EXISTS(SELECT 1 FROM audit_events WHERE action='drill.completed' AND resource_id='dr-operation')`).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored history verification failed")
	}
	sum := sha256.Sum256([]byte(content))
	fmt.Println(hex.EncodeToString(sum[:]))
	return nil
}

func credentialPayload(kind string) []byte {
	if kind == "git-https" {
		return []byte(`{"username":"drill","token":"synthetic-git-token"}`)
	}
	return []byte(`{"token":"synthetic-kubernetes-token"}`)
}
