package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"go.opentelemetry.io/otel"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, connectionString string) (*Store, error) {
	connectionConfig, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, errors.New("open PostgreSQL connection")
	}
	connectionConfig.Tracer = NewQueryTracer(otel.GetTracerProvider())
	db := stdlib.OpenDB(*connectionConfig)
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	s := &Store{DB: db}
	if err := s.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	for _, filename := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(filename, "migrations/"), ".sql")
		var applied bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}
		script, err := migrationFiles.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		if _, err = tx.ExecContext(ctx, string(script)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	IsAdmin     bool       `json:"isAdmin"`
	Disabled    bool       `json:"disabled"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Session struct {
	UserID   string
	CSRFHash []byte
	Expires  time.Time
}

var ErrAlreadyInitialized = errors.New("instance already has a user")

var (
	ErrLastInstanceAdmin         = errors.New("the instance must keep at least one active administrator")
	ErrUserDeleted               = errors.New("deleted users cannot be changed")
	ErrSystemUser                = errors.New("the system user cannot be changed")
	ErrLastWorkspaceOwner        = errors.New("a workspace must keep at least one owner")
	ErrWorkspaceMemberNotFound   = errors.New("workspace member not found")
	ErrWorkspaceMemberManagedSSO = errors.New("workspace member access is managed by SSO")
	ErrWorkspaceNotFound         = errors.New("workspace not found")
	ErrConnectionNotShared       = errors.New("connection is not shared with this workspace")
	ErrUserUnavailable           = errors.New("user is locked or deleted")
	ErrUserLastWorkspaceOwner    = errors.New("transfer ownership of every workspace before locking or deleting this user")
)

func (s *Store) SignupAvailable(ctx context.Context) (bool, error) {
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id <> 'justcd-system')`).Scan(&exists)
	return !exists, err
}

func (s *Store) CreateInitialAdmin(ctx context.Context, user *User, passwordHash string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize competing first-user signups, including requests to different API replicas.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id <> 'justcd-system')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAlreadyInitialized
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,is_admin) VALUES($1,LOWER($2),$3,$4,TRUE) RETURNING created_at`, user.ID, user.Email, user.DisplayName, passwordHash).Scan(&user.CreatedAt)
	if err != nil {
		return fmt.Errorf("create initial administrator: %w", err)
	}
	return tx.Commit()
}

func (s *Store) EnsureSystemActor(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name,is_admin,disabled) VALUES('justcd-system','system@justcd.invalid','JustCD automatic reconciler',FALSE,TRUE) ON CONFLICT(id) DO NOTHING`)
	return err
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var passwordHash sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at,password_hash FROM users WHERE LOWER(email)=LOWER($1) AND disabled=FALSE AND deleted_at IS NULL`, email).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt, &passwordHash)
	return user, passwordHash.String, err
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var user User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at FROM users WHERE id=$1 AND disabled=FALSE AND deleted_at IS NULL`, id).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt)
	return user, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash, csrfHash []byte, userID string, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,$4)`, tokenHash, csrfHash, userID, expires)
	return err
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var session Session
	err := s.DB.QueryRowContext(ctx, `SELECT user_id,csrf_hash,expires_at FROM sessions WHERE token_hash=$1 AND expires_at>NOW()`, tokenHash).
		Scan(&session.UserID, &session.CSRFHash, &session.Expires)
	return session, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	return err
}

type OIDCProvider struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"clientId"`
	RedirectURL  string    `json:"redirectUrl"`
	GroupsClaim  string    `json:"groupsClaim"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
	ClientSecret []byte    `json:"-"`
}

func (s *Store) CreateOIDCProvider(ctx context.Context, p OIDCProvider) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_providers(id,name,issuer,client_id,client_secret_cipher,redirect_url,groups_claim,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.ID, p.Name, p.Issuer, p.ClientID, p.ClientSecret, p.RedirectURL, p.GroupsClaim, p.Enabled)
	return err
}

func (s *Store) OIDCProviderByID(ctx context.Context, id string) (OIDCProvider, error) {
	var p OIDCProvider
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,issuer,client_id,client_secret_cipher,redirect_url,groups_claim,enabled,created_at FROM oidc_providers WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.RedirectURL, &p.GroupsClaim, &p.Enabled, &p.CreatedAt)
	return p, err
}

func (s *Store) ListOIDCProviders(ctx context.Context) ([]OIDCProvider, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,issuer,client_id,redirect_url,groups_claim,enabled,created_at FROM oidc_providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OIDCProvider, 0)
	for rows.Next() {
		var p OIDCProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Issuer, &p.ClientID, &p.RedirectURL, &p.GroupsClaim, &p.Enabled, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateOIDCState(ctx context.Context, stateHash []byte, providerID, nonce, verifier string, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_login_states(state_hash,provider_id,nonce,verifier,expires_at) VALUES($1,$2,$3,$4,$5)`, stateHash, providerID, nonce, verifier, expires)
	return err
}

type OIDCState struct {
	ProviderID string
	Nonce      string
	Verifier   string
}

func (s *Store) ConsumeOIDCState(ctx context.Context, stateHash []byte) (OIDCState, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return OIDCState{}, err
	}
	defer tx.Rollback()
	var out OIDCState
	err = tx.QueryRowContext(ctx, `DELETE FROM oidc_login_states WHERE state_hash=$1 AND expires_at>NOW() RETURNING provider_id,nonce,verifier`, stateHash).Scan(&out.ProviderID, &out.Nonce, &out.Verifier)
	if err != nil {
		return OIDCState{}, err
	}
	return out, tx.Commit()
}

func (s *Store) ResolveOIDCUser(ctx context.Context, providerID, subject, email, displayName string, groups []string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var user User
	created, linked := false, false
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.display_name,u.is_admin,u.created_at FROM oidc_identities i JOIN users u ON u.id=i.user_id WHERE i.provider_id=$1 AND i.subject=$2 AND u.disabled=FALSE AND u.deleted_at IS NULL`, providerID, subject).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at FROM users WHERE LOWER(email)=LOWER($1) AND disabled=FALSE AND deleted_at IS NULL`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt)
		if err == sql.ErrNoRows {
			user.ID = NewID()
			user.Email = email
			user.DisplayName = displayName
			created = true
			err = tx.QueryRowContext(ctx, `INSERT INTO users(id,email,display_name) VALUES($1,LOWER($2),$3) RETURNING created_at`, user.ID, email, displayName).Scan(&user.CreatedAt)
			if err != nil {
				return User{}, err
			}
		} else if err != nil {
			return User{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO oidc_identities(provider_id,subject,user_id) VALUES($1,$2,$3)`, providerID, subject, user.ID); err != nil {
			return User{}, err
		}
		linked = true
	} else if err != nil {
		return User{}, err
	}
	profileUpdated := false
	if displayName != "" {
		result, updateErr := tx.ExecContext(ctx, `UPDATE users SET display_name=$2 WHERE id=$1 AND display_name=''`, user.ID, displayName)
		if updateErr != nil {
			return User{}, updateErr
		}
		count, _ := result.RowsAffected()
		profileUpdated = count > 0 && !created
		if count > 0 {
			user.DisplayName = displayName
		}
	}
	var previousGrants []byte
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_object_agg(workspace_id,role),'{}'::jsonb) FROM oidc_membership_grants WHERE provider_id=$1 AND user_id=$2`, providerID, user.ID).Scan(&previousGrants); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM oidc_membership_grants WHERE provider_id=$1 AND user_id=$2`, providerID, user.ID); err != nil {
		return User{}, err
	}
	if len(groups) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO oidc_membership_grants(provider_id,user_id,workspace_id,role)
			SELECT provider_id,$2,workspace_id,CASE
			WHEN BOOL_OR(role='owner') THEN 'owner'
			WHEN BOOL_OR(role='deployer') THEN 'deployer'
			ELSE 'viewer' END
			FROM oidc_group_roles WHERE provider_id=$1 AND group_name=ANY($3) GROUP BY provider_id,workspace_id
			ON CONFLICT(provider_id,user_id,workspace_id) DO UPDATE SET role=EXCLUDED.role`, providerID, user.ID, groups); err != nil {
			return User{}, err
		}
	}
	var currentGrants []byte
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_object_agg(workspace_id,role),'{}'::jsonb) FROM oidc_membership_grants WHERE provider_id=$1 AND user_id=$2`, providerID, user.ID).Scan(&currentGrants); err != nil {
		return User{}, err
	}
	if created {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'user.oidc_provisioned','user',$1,jsonb_build_object('email',$2::text,'providerId',$3::text))`, user.ID, user.Email, providerID); err != nil {
			return User{}, err
		}
	} else if linked {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'user.oidc_linked','user',$1,jsonb_build_object('providerId',$2::text))`, user.ID, providerID); err != nil {
			return User{}, err
		}
	}
	if profileUpdated {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'user.oidc_profile_updated','user',$1,jsonb_build_object('displayName',$2::text,'providerId',$3::text))`, user.ID, displayName, providerID); err != nil {
			return User{}, err
		}
	}
	if string(previousGrants) != string(currentGrants) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'user.oidc_access_updated','user',$1,jsonb_build_object('providerId',$2::text,'previousRoles',$3::jsonb,'roles',$4::jsonb))`, user.ID, providerID, string(previousGrants), string(currentGrants)); err != nil {
			return User{}, err
		}
	}
	return user, tx.Commit()
}

func (s *Store) AddOIDCGroupRole(ctx context.Context, providerID, groupName, workspaceID, role string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_group_roles(provider_id,group_name,workspace_id,role) VALUES($1,$2,$3,$4) ON CONFLICT(provider_id,group_name,workspace_id) DO UPDATE SET role=EXCLUDED.role`, providerID, groupName, workspaceID, role)
	return err
}

func (s *Store) CreateUser(ctx context.Context, user User, passwordHash string) (User, error) {
	err := s.DB.QueryRowContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,is_admin) VALUES($1,LOWER($2),$3,$4,$5) RETURNING created_at`, user.ID, user.Email, user.DisplayName, passwordHash, user.IsAdmin).Scan(&user.CreatedAt)
	return user, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,email,display_name,is_admin,disabled,deleted_at,created_at FROM users WHERE id <> 'justcd-system' ORDER BY deleted_at NULLS FIRST, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var u User
		var deletedAt sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.Disabled, &deletedAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		if deletedAt.Valid {
			u.DeletedAt = &deletedAt.Time
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func lockActiveAdmins(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE is_admin=TRUE AND disabled=FALSE AND deleted_at IS NULL ORDER BY id FOR UPDATE`)
	if err != nil {
		return 0, err
	}
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	return count, rows.Close()
}

func (s *Store) UpdateAdminUser(ctx context.Context, id, email, displayName string, isAdmin, disabled bool, passwordHash string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	if err := lockUserWorkspaceRows(ctx, tx, id); err != nil {
		return User{}, err
	}
	activeAdmins, err := lockActiveAdmins(ctx, tx)
	if err != nil {
		return User{}, err
	}
	var wasAdmin, wasDisabled bool
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT is_admin,disabled,deleted_at FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&wasAdmin, &wasDisabled, &deletedAt)
	if err != nil {
		return User{}, err
	}
	if id == "justcd-system" {
		return User{}, ErrSystemUser
	}
	if deletedAt.Valid {
		return User{}, ErrUserDeleted
	}
	if wasAdmin && !wasDisabled && (!isAdmin || disabled) && activeAdmins <= 1 {
		return User{}, ErrLastInstanceAdmin
	}
	if disabled && !wasDisabled {
		lastOwner, err := userIsLastActiveWorkspaceOwner(ctx, tx, id)
		if err != nil {
			return User{}, err
		}
		if lastOwner {
			return User{}, ErrUserLastWorkspaceOwner
		}
	}

	query := `UPDATE users SET email=LOWER($2),display_name=$3,is_admin=$4,disabled=$5,
		password_hash=CASE WHEN $6='' THEN password_hash ELSE $6 END
		WHERE id=$1 AND deleted_at IS NULL
		RETURNING id,email,display_name,is_admin,disabled,deleted_at,created_at`
	var user User
	var updatedDeletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, query, id, email, displayName, isAdmin, disabled, passwordHash).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.Disabled, &updatedDeletedAt, &user.CreatedAt)
	if err != nil {
		return User{}, err
	}
	if updatedDeletedAt.Valid {
		user.DeletedAt = &updatedDeletedAt.Time
	}
	if disabled || passwordHash != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Store) DeleteAdminUser(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := lockUserWorkspaceRows(ctx, tx, id); err != nil {
		return err
	}
	activeAdmins, err := lockActiveAdmins(ctx, tx)
	if err != nil {
		return err
	}
	var wasAdmin, wasDisabled bool
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT is_admin,disabled,deleted_at FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&wasAdmin, &wasDisabled, &deletedAt)
	if err != nil {
		return err
	}
	if id == "justcd-system" {
		return ErrSystemUser
	}
	if deletedAt.Valid {
		return ErrUserDeleted
	}
	if wasAdmin && !wasDisabled && activeAdmins <= 1 {
		return ErrLastInstanceAdmin
	}
	if !wasDisabled {
		lastOwner, err := userIsLastActiveWorkspaceOwner(ctx, tx, id)
		if err != nil {
			return err
		}
		if lastOwner {
			return ErrUserLastWorkspaceOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_memberships WHERE user_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oidc_membership_grants WHERE user_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oidc_identities WHERE user_id=$1`, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET email='deleted+' || id || '@deleted.justcd.invalid',display_name='Deleted user',password_hash=NULL,is_admin=FALSE,disabled=TRUE,deleted_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetWorkspaceMember(ctx context.Context, workspaceID, userID, role string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return err
	}
	var currentRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&currentRole)
	if err == sql.ErrNoRows {
		if err := ensureActiveUser(ctx, tx, userID); err != nil {
			return err
		}
		var managedBySSO bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2)`, workspaceID, userID).Scan(&managedBySSO); err != nil {
			return err
		}
		if managedBySSO {
			return ErrWorkspaceMemberManagedSSO
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES($1,$2,$3)`, workspaceID, userID, role); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		var managedBySSO bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2)`, workspaceID, userID).Scan(&managedBySSO); err != nil {
			return err
		}
		if managedBySSO {
			return ErrWorkspaceMemberManagedSSO
		}
		var targetActive bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND disabled=FALSE AND deleted_at IS NULL)`, userID).Scan(&targetActive); err != nil {
			return err
		}
		if currentRole == "owner" && role != "owner" && targetActive {
			owners, err := workspaceOwnerCountTx(ctx, tx, workspaceID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return ErrLastWorkspaceOwner
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workspace_memberships SET role=$3 WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RemoveWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return err
	}
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		var managedBySSO bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2)`, workspaceID, userID).Scan(&managedBySSO); err != nil {
			return err
		}
		if managedBySSO {
			return ErrWorkspaceMemberManagedSSO
		}
		return ErrWorkspaceMemberNotFound
	}
	if err != nil {
		return err
	}
	var managedBySSO bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2)`, workspaceID, userID).Scan(&managedBySSO); err != nil {
		return err
	}
	if managedBySSO {
		return ErrWorkspaceMemberManagedSSO
	}
	var targetActive bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND disabled=FALSE AND deleted_at IS NULL)`, userID).Scan(&targetActive); err != nil {
		return err
	}
	if role == "owner" && targetActive {
		owners, err := workspaceOwnerCountTx(ctx, tx, workspaceID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastWorkspaceOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func lockWorkspace(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspaceID).Scan(&id)
	if err == sql.ErrNoRows {
		return ErrWorkspaceNotFound
	}
	return err
}

func lockUserWorkspaceRows(ctx context.Context, tx *sql.Tx, userID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM workspaces p WHERE
		EXISTS(SELECT 1 FROM workspace_memberships pm WHERE pm.workspace_id=p.id AND pm.user_id=$1) OR
		EXISTS(SELECT 1 FROM oidc_membership_grants gm WHERE gm.workspace_id=p.id AND gm.user_id=$1)
		ORDER BY p.id FOR UPDATE OF p`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var workspaceID string
		if err := rows.Scan(&workspaceID); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

func userIsLastActiveWorkspaceOwner(ctx context.Context, tx *sql.Tx, userID string) (bool, error) {
	var lastOwner bool
	err := tx.QueryRowContext(ctx, `WITH effective AS (
		SELECT workspace_id,user_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank
		FROM (
			SELECT workspace_id,user_id,role FROM workspace_memberships
			UNION ALL SELECT workspace_id,user_id,role FROM oidc_membership_grants
		) grants GROUP BY workspace_id,user_id
	), owners AS (
		SELECT effective.workspace_id,effective.user_id FROM effective
		JOIN users u ON u.id=effective.user_id
		WHERE effective.rank=3 AND u.disabled=FALSE AND u.deleted_at IS NULL
	)
	SELECT EXISTS(
		SELECT workspace_id FROM owners GROUP BY workspace_id
		HAVING COUNT(*)=1 AND BOOL_OR(user_id=$1)
	)`, userID).Scan(&lastOwner)
	return lastOwner, err
}

func ensureActiveUser(ctx context.Context, tx *sql.Tx, userID string) error {
	var activeID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND disabled=FALSE AND deleted_at IS NULL FOR SHARE`, userID).Scan(&activeID)
	if err == sql.ErrNoRows {
		return ErrUserUnavailable
	}
	return err
}

func workspaceOwnerCountTx(ctx context.Context, tx *sql.Tx, workspaceID string) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT user_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank
		FROM (
			SELECT user_id,role FROM workspace_memberships WHERE workspace_id=$1
			UNION ALL SELECT user_id,role FROM oidc_membership_grants WHERE workspace_id=$1
		) members GROUP BY user_id
	) effective JOIN users u ON u.id=effective.user_id WHERE effective.rank=3 AND u.disabled=FALSE AND u.deleted_at IS NULL`, workspaceID).Scan(&count)
	return count, err
}

func (s *Store) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `WITH effective AS (
		SELECT user_id,role FROM workspace_memberships WHERE workspace_id=$1
		UNION ALL SELECT user_id,role FROM oidc_membership_grants WHERE workspace_id=$1
	), ranked AS (
		SELECT user_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank FROM effective GROUP BY user_id
	)
	SELECT u.id,u.email,u.display_name,CASE ranked.rank WHEN 3 THEN 'owner' WHEN 2 THEN 'deployer' ELSE 'viewer' END,
		u.disabled,
		EXISTS(SELECT 1 FROM workspace_memberships direct WHERE direct.workspace_id=$1 AND direct.user_id=u.id),
		EXISTS(SELECT 1 FROM oidc_membership_grants sso WHERE sso.workspace_id=$1 AND sso.user_id=u.id)
	FROM ranked JOIN users u ON u.id=ranked.user_id ORDER BY u.email`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, email, name, role string
		var disabled, hasDirect, managedBySSO bool
		if err := rows.Scan(&id, &email, &name, &role, &disabled, &hasDirect, &managedBySSO); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "email": email, "displayName": name, "role": role, "disabled": disabled, "managedBySSO": managedBySSO, "editable": hasDirect && !managedBySSO})
	}
	return out, rows.Err()
}

type Workspace struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Role           string         `json:"role,omitempty"`
	ApprovalPolicy ApprovalPolicy `json:"approvalPolicy"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type ApprovalRule struct {
	RequiredApprovals int      `json:"requiredApprovals"`
	ApproverRoles     []string `json:"approverRoles"`
	ApproverUserIDs   []string `json:"approverUserIds"`
}

type ApprovalPolicy struct {
	Sync     ApprovalRule `json:"sync"`
	Deletion ApprovalRule `json:"deletion"`
}

type ApprovalPolicyOverride struct {
	Sync     *ApprovalRule `json:"sync,omitempty"`
	Deletion *ApprovalRule `json:"deletion,omitempty"`
}

func ApprovalRuleAllows(rule ApprovalRule, role, userID string) bool {
	for _, allowedID := range rule.ApproverUserIDs {
		if allowedID == userID && userID != "" {
			return true
		}
	}
	rank := func(value string) int {
		switch value {
		case "owner":
			return 3
		case "deployer":
			return 2
		case "viewer":
			return 1
		default:
			return 0
		}
	}
	if len(rule.ApproverRoles) == 0 && len(rule.ApproverUserIDs) == 0 && rule.RequiredApprovals > 0 {
		return role == "owner"
	}
	for _, allowedRole := range rule.ApproverRoles {
		if rank(role) >= rank(allowedRole) && rank(role) > 0 {
			return true
		}
	}
	return false
}

func DefaultApprovalPolicy() ApprovalPolicy {
	return ApprovalPolicy{
		Sync:     ApprovalRule{RequiredApprovals: 0, ApproverRoles: []string{"owner"}, ApproverUserIDs: []string{}},
		Deletion: ApprovalRule{RequiredApprovals: 1, ApproverRoles: []string{"owner"}, ApproverUserIDs: []string{}},
	}
}

func (s *Store) ListWorkspaces(ctx context.Context, user User) ([]Workspace, error) {
	query := `WITH effective AS (
		SELECT workspace_id,user_id,role FROM workspace_memberships WHERE user_id=$1
		UNION ALL
		SELECT workspace_id,user_id,role FROM oidc_membership_grants WHERE user_id=$1
	), ranked AS (
		SELECT workspace_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank FROM effective GROUP BY workspace_id
	)
	SELECT p.id,p.name,p.description,CASE COALESCE(ranked.rank,3) WHEN 3 THEN 'owner' WHEN 2 THEN 'deployer' ELSE 'viewer' END,p.approval_policy,p.created_at
	FROM workspaces p LEFT JOIN ranked ON ranked.workspace_id=p.id WHERE $2 OR ranked.workspace_id IS NOT NULL ORDER BY p.name`
	rows, err := s.DB.QueryContext(ctx, query, user.ID, user.IsAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := make([]Workspace, 0)
	for rows.Next() {
		var workspace Workspace
		var rawPolicy []byte
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Description, &workspace.Role, &rawPolicy, &workspace.CreatedAt); err != nil {
			return nil, err
		}
		workspace.ApprovalPolicy = DefaultApprovalPolicy()
		if err := json.Unmarshal(rawPolicy, &workspace.ApprovalPolicy); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (s *Store) WorkspaceByID(ctx context.Context, id string) (Workspace, error) {
	var workspace Workspace
	var rawPolicy []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,description,approval_policy,created_at FROM workspaces WHERE id=$1`, id).Scan(&workspace.ID, &workspace.Name, &workspace.Description, &rawPolicy, &workspace.CreatedAt)
	if err != nil {
		return Workspace{}, err
	}
	workspace.ApprovalPolicy = DefaultApprovalPolicy()
	if err := json.Unmarshal(rawPolicy, &workspace.ApprovalPolicy); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (s *Store) CreateWorkspace(ctx context.Context, workspace Workspace, ownerID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspaces(id,name,description) VALUES($1,$2,$3)`, workspace.ID, workspace.Name, workspace.Description); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace.ID, ownerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateWorkspace(ctx context.Context, id, name, description string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE workspaces SET name=$2,description=$3,updated_at=NOW() WHERE id=$1`, id, name, description)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateWorkspaceApprovalPolicy(ctx context.Context, id string, policy ApprovalPolicy) error {
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET approval_policy=$2,updated_at=NOW() WHERE id=$1`, id, encoded); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE workspace_id=$1)`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteWorkspaceKeepingResources(ctx context.Context, id string, requireEmpty bool) (int, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return 0, 0, err
	}
	var apps, managed, active int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM applications WHERE workspace_id=$1), (SELECT COUNT(*) FROM managed_resources m JOIN applications a ON a.id=m.application_id WHERE a.workspace_id=$1), (SELECT COUNT(*) FROM operations o JOIN applications a ON a.id=o.application_id WHERE a.workspace_id=$1 AND o.status IN ('queued','running'))`, id).Scan(&apps, &managed, &active); err != nil {
		return 0, 0, err
	}
	if active > 0 {
		return 0, 0, errors.New("workspace has active sync operations")
	}
	if requireEmpty && managed > 0 {
		return 0, 0, errors.New("workspace still manages Kubernetes resources")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, id); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return apps, managed, nil
}

func (s *Store) WorkspaceRole(ctx context.Context, user User, workspaceID string) (string, error) {
	if user.IsAdmin {
		return "owner", nil
	}
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT role FROM (
		SELECT role FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2
		UNION ALL SELECT role FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2
	) roles ORDER BY CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END DESC LIMIT 1`, workspaceID, user.ID).Scan(&role)
	return role, err
}

func (s *Store) WorkspaceRoleForUser(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT role FROM (
		SELECT role FROM workspace_memberships WHERE workspace_id=$1 AND user_id=$2
		UNION ALL SELECT role FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=$2
	) roles ORDER BY CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END DESC LIMIT 1`, workspaceID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func (s *Store) WorkspaceOwnerCount(ctx context.Context, workspaceID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT user_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank
		FROM (
			SELECT user_id,role FROM workspace_memberships WHERE workspace_id=$1
			UNION ALL SELECT user_id,role FROM oidc_membership_grants WHERE workspace_id=$1
		) members GROUP BY user_id
	) effective JOIN users u ON u.id=effective.user_id WHERE effective.rank=3 AND u.disabled=FALSE AND u.deleted_at IS NULL`, workspaceID).Scan(&count)
	return count, err
}

type Credential struct {
	ID          string     `json:"id"`
	WorkspaceID *string    `json:"workspaceId,omitempty"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Username    string     `json:"username,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	Cipher      []byte     `json:"-"`
}

func (s *Store) CreateCredential(ctx context.Context, c Credential) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, c.ID, c.WorkspaceID, c.Name, c.Kind, c.Cipher, c.ExpiresAt)
	return err
}

func (s *Store) UpdateCredential(ctx context.Context, c Credential) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE credentials SET name=$2,secret_cipher=$3,expires_at=$4 WHERE id=$1`, c.ID, c.Name, c.Cipher, c.ExpiresAt)
}

func (s *Store) updateConnectionAndInvalidate(ctx context.Context, query string, args ...any) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current'`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CredentialByID(ctx context.Context, id string) (Credential, error) {
	var c Credential
	err := s.DB.QueryRowContext(ctx, `SELECT id,workspace_id,name,kind,secret_cipher,expires_at,created_at FROM credentials WHERE id=$1`, id).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Kind, &c.Cipher, &c.ExpiresAt, &c.CreatedAt)
	return c, err
}

func (s *Store) ListCredentials(ctx context.Context, workspaceID string) ([]Credential, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,workspace_id,name,kind,secret_cipher,expires_at,created_at FROM credentials WHERE workspace_id=$1 OR workspace_id IS NULL ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Credential, 0)
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Kind, &c.Cipher, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

type Cluster struct {
	ID                      string    `json:"id"`
	WorkspaceID             *string   `json:"workspaceId,omitempty"`
	Shared                  bool      `json:"shared,omitempty"`
	OwnerWorkspaceName      string    `json:"ownerWorkspaceName,omitempty"`
	Name                    string    `json:"name"`
	APIServer               string    `json:"apiServer"`
	CAData                  []byte    `json:"-"`
	InsecureSkipVerify      bool      `json:"insecureSkipVerify"`
	DefaultCredentialID     *string   `json:"defaultCredentialId,omitempty"`
	ClusterScopeCredential  *string   `json:"clusterScopeCredentialId,omitempty"`
	MaxConcurrentOperations int       `json:"maxConcurrentOperations"`
	OperationsPerMinute     int       `json:"operationsPerMinute"`
	CreatedAt               time.Time `json:"createdAt"`
}

func (s *Store) CreateCluster(ctx context.Context, c Cluster) error {
	c = normalizeClusterLimits(c)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO clusters(id,workspace_id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,max_concurrent_operations,operations_per_minute) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, c.WorkspaceID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential, c.MaxConcurrentOperations, c.OperationsPerMinute)
	return err
}

func (s *Store) CreateClusterWithWorkspaceCredential(ctx context.Context, c Cluster, workspaceID, credentialID string) error {
	c = normalizeClusterLimits(c)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c.WorkspaceID = &workspaceID
	if _, err := tx.ExecContext(ctx, `INSERT INTO clusters(id,workspace_id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,max_concurrent_operations,operations_per_minute) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, c.WorkspaceID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential, c.MaxConcurrentOperations, c.OperationsPerMinute); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_cluster_credentials(workspace_id,cluster_id,credential_id) VALUES($1,$2,$3)`, workspaceID, c.ID, credentialID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateWorkspaceCluster(ctx context.Context, c Cluster) error {
	c = normalizeClusterLimits(c)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO clusters(id,workspace_id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,max_concurrent_operations,operations_per_minute) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, c.WorkspaceID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential, c.MaxConcurrentOperations, c.OperationsPerMinute)
	return err
}

func (s *Store) WorkspaceClusterCredential(ctx context.Context, workspaceID, clusterID string) (*string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT credential_id FROM workspace_cluster_credentials WHERE workspace_id=$1 AND cluster_id=$2`, workspaceID, clusterID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Store) SetWorkspaceClusterCredential(ctx context.Context, workspaceID, clusterID string, credentialID *string) error {
	if credentialID == nil {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM workspace_cluster_credentials WHERE workspace_id=$1 AND cluster_id=$2`, workspaceID, clusterID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO workspace_cluster_credentials(workspace_id,cluster_id,credential_id) VALUES($1,$2,$3) ON CONFLICT (workspace_id,cluster_id) DO UPDATE SET credential_id=EXCLUDED.credential_id,updated_at=NOW()`, workspaceID, clusterID, *credentialID)
	return err
}
func (s *Store) ClusterByID(ctx context.Context, id string) (Cluster, error) {
	var c Cluster
	err := s.DB.QueryRowContext(ctx, `SELECT id,workspace_id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,max_concurrent_operations,operations_per_minute,created_at FROM clusters WHERE id=$1`, id).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.APIServer, &c.CAData, &c.InsecureSkipVerify, &c.DefaultCredentialID, &c.ClusterScopeCredential, &c.MaxConcurrentOperations, &c.OperationsPerMinute, &c.CreatedAt)
	return c, err
}
func (s *Store) ListClusters(ctx context.Context, workspaceID string) ([]Cluster, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id,c.workspace_id,c.name,c.api_server,c.insecure_skip_verify,c.default_credential_id,c.cluster_scope_credential_id,c.max_concurrent_operations,c.operations_per_minute,(c.workspace_id IS NOT NULL AND c.workspace_id<>$1),COALESCE(owner.name,''),c.created_at
		FROM clusters c LEFT JOIN workspaces owner ON owner.id=c.workspace_id WHERE c.workspace_id IS NULL OR c.workspace_id=$1 OR EXISTS (
			SELECT 1 FROM workspace_cluster_shares sh WHERE sh.cluster_id=c.id AND sh.target_workspace_id=$1 AND sh.status='accepted'
		) ORDER BY c.name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Cluster, 0)
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.APIServer, &c.InsecureSkipVerify, &c.DefaultCredentialID, &c.ClusterScopeCredential, &c.MaxConcurrentOperations, &c.OperationsPerMinute, &c.Shared, &c.OwnerWorkspaceName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) WorkspaceCanUseCluster(ctx context.Context, workspaceID, clusterID string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM clusters c WHERE c.id=$2 AND (c.workspace_id IS NULL OR c.workspace_id=$1 OR EXISTS (
			SELECT 1 FROM workspace_cluster_shares sh WHERE sh.cluster_id=c.id AND sh.target_workspace_id=$1 AND sh.status='accepted'
		))
	)`, workspaceID, clusterID).Scan(&allowed)
	return allowed, err
}

func (s *Store) UpdateCluster(ctx context.Context, c Cluster) error {
	c = normalizeClusterLimits(c)
	return s.updateConnectionAndInvalidate(ctx, `UPDATE clusters SET name=$2,api_server=$3,ca_data=$4,insecure_skip_verify=$5,default_credential_id=$6,cluster_scope_credential_id=$7,max_concurrent_operations=$8,operations_per_minute=$9,updated_at=NOW() WHERE id=$1`, c.ID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential, c.MaxConcurrentOperations, c.OperationsPerMinute)
}

func normalizeClusterLimits(c Cluster) Cluster {
	if c.MaxConcurrentOperations == 0 {
		c.MaxConcurrentOperations = 2
	}
	if c.OperationsPerMinute == 0 {
		c.OperationsPerMinute = 30
	}
	return c
}

func (s *Store) CreateNamespaceBinding(ctx context.Context, workspaceID, clusterID, namespace string, credentialID *string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace,credential_id) VALUES($1,$2,$3,$4,$5)`, NewID(), workspaceID, clusterID, namespace, credentialID)
	return err
}

func (s *Store) UpdateNamespaceBinding(ctx context.Context, workspaceID, clusterID, namespace string, credentialID *string) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE namespace_bindings SET credential_id=$4 WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3`, workspaceID, clusterID, namespace, credentialID)
}

func (s *Store) NamespaceBinding(ctx context.Context, workspaceID, clusterID, namespace string) (NamespaceBinding, error) {
	var binding NamespaceBinding
	err := s.DB.QueryRowContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3`, workspaceID, clusterID, namespace).Scan(&binding.Namespace, &binding.CredentialID)
	return binding, err
}

func (s *Store) ListNamespaceBindings(ctx context.Context, workspaceID, clusterID string) ([]NamespaceBinding, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 ORDER BY namespace`, workspaceID, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]NamespaceBinding, 0)
	for rows.Next() {
		var b NamespaceBinding
		if err := rows.Scan(&b.Namespace, &b.CredentialID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type NamespaceBinding struct {
	Namespace    string  `json:"namespace"`
	CredentialID *string `json:"credentialId,omitempty"`
}

type KubernetesPermissionTest struct {
	WorkspaceID string          `json:"workspaceId"`
	ClusterID   string          `json:"clusterId"`
	Namespace   string          `json:"namespace"`
	Report      json.RawMessage `json:"report"`
	CheckedAt   time.Time       `json:"checkedAt"`
}

func (s *Store) SaveKubernetesPermissionTest(ctx context.Context, test KubernetesPermissionTest) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO kubernetes_permission_tests(workspace_id,cluster_id,namespace,report,checked_at)
		VALUES($1,$2,$3,$4::jsonb,$5)
		ON CONFLICT (workspace_id,cluster_id,namespace) DO UPDATE SET report=EXCLUDED.report,checked_at=EXCLUDED.checked_at`,
		test.WorkspaceID, test.ClusterID, test.Namespace, string(test.Report), test.CheckedAt)
	return err
}

func (s *Store) ListKubernetesPermissionTests(ctx context.Context, workspaceID, clusterID string) ([]KubernetesPermissionTest, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT workspace_id,cluster_id,namespace,report::text,checked_at
		FROM kubernetes_permission_tests WHERE workspace_id=$1 AND cluster_id=$2 ORDER BY namespace`, workspaceID, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KubernetesPermissionTest, 0)
	for rows.Next() {
		var item KubernetesPermissionTest
		if err := rows.Scan(&item.WorkspaceID, &item.ClusterID, &item.Namespace, &item.Report, &item.CheckedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type GitSource struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspaceId"`
	Name               string    `json:"name"`
	RepositoryURL      string    `json:"repositoryUrl"`
	CredentialID       *string   `json:"credentialId,omitempty"`
	Shared             bool      `json:"shared,omitempty"`
	OwnerWorkspaceName string    `json:"ownerWorkspaceName,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

func (s *Store) CreateGitSource(ctx context.Context, v GitSource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO git_sources(id,workspace_id,name,repository_url,credential_id) VALUES($1,$2,$3,$4,$5)`, v.ID, v.WorkspaceID, v.Name, v.RepositoryURL, v.CredentialID)
	return err
}
func (s *Store) UpdateGitSource(ctx context.Context, v GitSource) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE git_sources SET name=$2,repository_url=$3,credential_id=$4,updated_at=NOW() WHERE id=$1`, v.ID, v.Name, v.RepositoryURL, v.CredentialID)
}
func (s *Store) GitSourceByID(ctx context.Context, id string) (GitSource, error) {
	var v GitSource
	err := s.DB.QueryRowContext(ctx, `SELECT id,workspace_id,name,repository_url,credential_id,created_at FROM git_sources WHERE id=$1`, id).Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.RepositoryURL, &v.CredentialID, &v.CreatedAt)
	return v, err
}
func (s *Store) ListGitSources(ctx context.Context, workspaceID string) ([]GitSource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT g.id,g.workspace_id,g.name,g.repository_url,
		CASE WHEN g.workspace_id=$1 THEN g.credential_id ELSE sh.credential_id END,
		(g.workspace_id<>$1), owner.name, g.created_at
		FROM git_sources g JOIN workspaces owner ON owner.id=g.workspace_id
		LEFT JOIN workspace_git_source_shares sh ON sh.git_source_id=g.id AND sh.target_workspace_id=$1 AND sh.status='accepted'
		WHERE g.workspace_id=$1 OR sh.id IS NOT NULL ORDER BY g.name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GitSource, 0)
	for rows.Next() {
		var v GitSource
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.RepositoryURL, &v.CredentialID, &v.Shared, &v.OwnerWorkspaceName, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GitSourceForWorkspace(ctx context.Context, sourceID, workspaceID string) (GitSource, error) {
	source, err := s.GitSourceByID(ctx, sourceID)
	if err != nil {
		return GitSource{}, err
	}
	if source.WorkspaceID == workspaceID {
		return source, nil
	}
	err = s.DB.QueryRowContext(ctx, `SELECT credential_id FROM workspace_git_source_shares
		WHERE git_source_id=$1 AND target_workspace_id=$2 AND status='accepted'`, sourceID, workspaceID).Scan(&source.CredentialID)
	if errors.Is(err, sql.ErrNoRows) {
		return GitSource{}, ErrConnectionNotShared
	}
	if err != nil {
		return GitSource{}, err
	}
	source.Shared = true
	return source, nil
}

func (s *Store) WorkspaceCanUseGitSource(ctx context.Context, workspaceID, sourceID string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM git_sources g WHERE g.id=$2 AND (g.workspace_id=$1 OR EXISTS (
			SELECT 1 FROM workspace_git_source_shares sh WHERE sh.git_source_id=g.id AND sh.target_workspace_id=$1 AND sh.status='accepted'
		))
	)`, workspaceID, sourceID).Scan(&allowed)
	return allowed, err
}

type RetryPolicy struct {
	Enabled             bool `json:"enabled"`
	MaxAttempts         int  `json:"maxAttempts"`
	InitialDelaySeconds int  `json:"initialDelaySeconds"`
	MaxDelaySeconds     int  `json:"maxDelaySeconds"`
	JitterPercent       int  `json:"jitterPercent"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Enabled: true, MaxAttempts: 5, InitialDelaySeconds: 5, MaxDelaySeconds: 300, JitterPercent: 20}
}

func NormalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxAttempts == 0 && policy.InitialDelaySeconds == 0 && policy.MaxDelaySeconds == 0 {
		return DefaultRetryPolicy()
	}
	return policy
}

func (policy RetryPolicy) Validate() error {
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 20 {
		return errors.New("retry maxAttempts must be between 1 and 20")
	}
	if policy.InitialDelaySeconds < 1 || policy.InitialDelaySeconds > 3600 {
		return errors.New("retry initialDelaySeconds must be between 1 and 3600")
	}
	if policy.MaxDelaySeconds < policy.InitialDelaySeconds || policy.MaxDelaySeconds > 86400 {
		return errors.New("retry maxDelaySeconds must be at least the initial delay and no more than 86400")
	}
	if policy.JitterPercent < 0 || policy.JitterPercent > 50 {
		return errors.New("retry jitterPercent must be between 0 and 50")
	}
	return nil
}

type Application struct {
	RepositoryIgnoreRules          []core.IgnoreRule                  `json:"-"`
	RepositoryIgnoreSelectors      []core.IgnoreSelector              `json:"-"`
	HelmReleaseName                string                             `json:"helmReleaseName,omitempty"`
	RepositoryConfigurationID      string                             `json:"repositoryConfigurationId,omitempty"`
	ConfigurationPath              string                             `json:"configurationPath,omitempty"`
	ConfigurationCommit            string                             `json:"configurationCommit,omitempty"`
	ConfigurationHash              string                             `json:"-"`
	ConfigurationMissing           bool                               `json:"configurationMissing"`
	ID                             string                             `json:"id"`
	WorkspaceID                    string                             `json:"workspaceId"`
	Name                           string                             `json:"name"`
	SourceID                       string                             `json:"sourceId"`
	Revision                       string                             `json:"revision"`
	ManifestPath                   string                             `json:"manifestPath"`
	Renderer                       string                             `json:"renderer"`
	KustomizeHelmEnabled           bool                               `json:"kustomizeHelmEnabled"`
	KustomizeNamespaceOverride     bool                               `json:"kustomizeNamespaceOverride"`
	HelmValuesFiles                []string                           `json:"helmValuesFiles"`
	HelmValuesYAML                 string                             `json:"helmValuesYaml"`
	ApplicationGroupID             string                             `json:"applicationGroupId,omitempty"`
	TargetManifestPath             string                             `json:"targetManifestPath"`
	NamespaceManifestPaths         map[string]string                  `json:"namespaceManifestPaths"`
	TargetHelmValuesFiles          []string                           `json:"targetHelmValuesFiles"`
	TargetHelmValuesYAML           string                             `json:"targetHelmValuesYaml"`
	NamespaceHelmValues            map[string]core.HelmValuesOverride `json:"namespaceHelmValues"`
	ClusterID                      string                             `json:"clusterId"`
	Namespaces                     []NamespaceBinding                 `json:"namespaces"`
	SyncPolicy                     string                             `json:"syncPolicy"`
	PollSeconds                    int                                `json:"pollSeconds"`
	LastCheckedAt                  *time.Time                         `json:"lastCheckedAt,omitempty"`
	LastSyncedRevision             string                             `json:"lastSyncedRevision,omitempty"`
	Health                         string                             `json:"health"`
	StatusIssues                   []ApplicationStatusIssue           `json:"statusIssues"`
	HealthCondition                apphealth.ApplicationCondition     `json:"healthCondition"`
	Decommissioning                bool                               `json:"decommissioning"`
	AutoSyncPaused                 bool                               `json:"autoSyncPaused"`
	RollbackResumeAvailable        bool                               `json:"rollbackResumeAvailable"`
	RollbackResumeRequiresRevision bool                               `json:"rollbackResumeRequiresRevision"`
	RollbackResumeState            *ApplicationRollbackState          `json:"-"`
	ApprovalPolicyOverride         *ApprovalPolicyOverride            `json:"approvalPolicyOverride,omitempty"`
	RetryPolicy                    RetryPolicy                        `json:"retryPolicy"`
	RetryAttemptCount              int                                `json:"retryAttemptCount"`
	RetryNextAt                    *time.Time                         `json:"retryNextAt,omitempty"`
	RetryTerminalReason            string                             `json:"retryTerminalReason,omitempty"`
	RetryLastErrorCode             string                             `json:"retryLastErrorCode,omitempty"`
	CreatedAt                      time.Time                          `json:"createdAt"`
}

type ApplicationStatusIssue struct {
	Source     string    `json:"source"`
	Summary    string    `json:"summary"`
	ObservedAt time.Time `json:"observedAt"`
}

type ApplicationGroup struct {
	ID                         string    `json:"id"`
	WorkspaceID                string    `json:"workspaceId"`
	Name                       string    `json:"name"`
	SourceID                   string    `json:"sourceId"`
	Revision                   string    `json:"revision"`
	ManifestPath               string    `json:"manifestPath"`
	Renderer                   string    `json:"renderer"`
	KustomizeHelmEnabled       bool      `json:"kustomizeHelmEnabled"`
	KustomizeNamespaceOverride bool      `json:"kustomizeNamespaceOverride"`
	HelmValuesFiles            []string  `json:"helmValuesFiles"`
	HelmValuesYAML             string    `json:"helmValuesYaml"`
	SyncPolicy                 string    `json:"syncPolicy"`
	PollSeconds                int       `json:"pollSeconds"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

// ApplicationRollbackState is non-secret configuration needed to return to
// the tracked source after a successful rollback pin.
type ApplicationRollbackState struct {
	HelmReleaseName            string                             `json:"helmReleaseName,omitempty"`
	SourceID                   string                             `json:"sourceId"`
	Revision                   string                             `json:"revision"`
	ManifestPath               string                             `json:"manifestPath"`
	TargetManifestPath         string                             `json:"targetManifestPath"`
	NamespaceManifestPaths     map[string]string                  `json:"namespaceManifestPaths"`
	Renderer                   string                             `json:"renderer"`
	KustomizeHelmEnabled       bool                               `json:"kustomizeHelmEnabled"`
	KustomizeNamespaceOverride bool                               `json:"kustomizeNamespaceOverride"`
	HelmValuesFiles            []string                           `json:"helmValuesFiles"`
	HelmValuesYAML             string                             `json:"helmValuesYaml"`
	TargetHelmValuesFiles      []string                           `json:"targetHelmValuesFiles"`
	TargetHelmValuesYAML       string                             `json:"targetHelmValuesYaml"`
	NamespaceHelmValues        map[string]core.HelmValuesOverride `json:"namespaceHelmValues"`
	ClusterID                  string                             `json:"clusterId"`
	Namespaces                 []NamespaceBinding                 `json:"namespaces"`
	SyncPolicy                 string                             `json:"syncPolicy"`
	PollSeconds                int                                `json:"pollSeconds"`
}

type applicationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) CreateApplication(ctx context.Context, a Application) error {
	return createApplication(ctx, s.DB, a)
}

func createApplication(ctx context.Context, executor applicationExecutor, a Application) error {
	a.RetryPolicy = NormalizeRetryPolicy(a.RetryPolicy)
	if err := a.RetryPolicy.Validate(); err != nil {
		return err
	}
	namespaces, err := json.Marshal(a.Namespaces)
	if err != nil {
		return err
	}
	valuesFiles, err := json.Marshal(a.HelmValuesFiles)
	if err != nil {
		return err
	}
	targetValuesFiles, err := json.Marshal(a.TargetHelmValuesFiles)
	if err != nil {
		return err
	}
	namespaceManifestPaths, err := json.Marshal(a.NamespaceManifestPaths)
	if err != nil {
		return err
	}
	if a.NamespaceManifestPaths == nil {
		namespaceManifestPaths = []byte(`{}`)
	}
	namespaceValues, err := json.Marshal(a.NamespaceHelmValues)
	if err != nil {
		return err
	}
	if a.NamespaceHelmValues == nil {
		namespaceValues = []byte(`{}`)
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,helm_values_files,helm_values_yaml,application_group_id,target_manifest_path,namespace_manifest_paths,target_helm_values_files,target_helm_values_yaml,namespace_helm_values,cluster_id,namespaces,sync_policy,poll_seconds,retry_enabled,retry_max_attempts,retry_initial_delay_seconds,retry_max_delay_seconds,retry_jitter_percent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`, a.ID, a.WorkspaceID, a.Name, a.SourceID, a.Revision, a.ManifestPath, a.Renderer, a.KustomizeHelmEnabled, a.KustomizeNamespaceOverride, valuesFiles, a.HelmValuesYAML, a.ApplicationGroupID, a.TargetManifestPath, namespaceManifestPaths, targetValuesFiles, a.TargetHelmValuesYAML, namespaceValues, a.ClusterID, namespaces, a.SyncPolicy, a.PollSeconds, a.RetryPolicy.Enabled, a.RetryPolicy.MaxAttempts, a.RetryPolicy.InitialDelaySeconds, a.RetryPolicy.MaxDelaySeconds, a.RetryPolicy.JitterPercent)
	return err
}

func scanApplication(row interface{ Scan(...any) error }) (Application, error) {
	var a Application
	var namespaces, helmValuesFiles, namespaceManifestPaths, targetValuesFiles, namespaceValues, rawApprovalOverride, rawRollbackState, rawStatusIssues, rawHealthResources, rawHealthWarnings []byte
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.SourceID, &a.Revision, &a.ManifestPath, &a.Renderer, &a.KustomizeHelmEnabled, &a.KustomizeNamespaceOverride, &helmValuesFiles, &a.HelmValuesYAML, &a.ApplicationGroupID, &a.TargetManifestPath, &namespaceManifestPaths, &targetValuesFiles, &a.TargetHelmValuesYAML, &namespaceValues, &a.ClusterID, &namespaces, &a.SyncPolicy, &a.PollSeconds, &a.LastCheckedAt, &a.LastSyncedRevision, &a.Health, &rawStatusIssues, &a.Decommissioning, &rawApprovalOverride, &a.AutoSyncPaused, &rawRollbackState, &a.RollbackResumeRequiresRevision, &a.CreatedAt, &a.RetryPolicy.Enabled, &a.RetryPolicy.MaxAttempts, &a.RetryPolicy.InitialDelaySeconds, &a.RetryPolicy.MaxDelaySeconds, &a.RetryPolicy.JitterPercent, &a.RetryAttemptCount, &a.RetryNextAt, &a.RetryTerminalReason, &a.RetryLastErrorCode, &a.HealthCondition.Status, &a.HealthCondition.Reason, &a.HealthCondition.Message, &a.HealthCondition.LastTransitionTime, &a.HealthCondition.ObservedAt, &rawHealthResources, &rawHealthWarnings, &a.RepositoryConfigurationID, &a.ConfigurationPath, &a.ConfigurationCommit, &a.ConfigurationHash, &a.ConfigurationMissing, &a.HelmReleaseName)
	if err == nil {
		err = json.Unmarshal(rawStatusIssues, &a.StatusIssues)
	}
	if err == nil {
		err = json.Unmarshal(rawHealthResources, &a.HealthCondition.Resources)
	}
	if err == nil {
		err = json.Unmarshal(rawHealthWarnings, &a.HealthCondition.Warnings)
	}
	if err == nil {
		err = json.Unmarshal(namespaces, &a.Namespaces)
	}
	if err == nil {
		err = json.Unmarshal(helmValuesFiles, &a.HelmValuesFiles)
	}
	if err == nil {
		err = json.Unmarshal(namespaceManifestPaths, &a.NamespaceManifestPaths)
	}
	if err == nil {
		err = json.Unmarshal(targetValuesFiles, &a.TargetHelmValuesFiles)
	}
	if err == nil {
		err = json.Unmarshal(namespaceValues, &a.NamespaceHelmValues)
	}
	if a.HelmValuesFiles == nil {
		a.HelmValuesFiles = []string{}
	}
	if a.StatusIssues == nil {
		a.StatusIssues = []ApplicationStatusIssue{}
	}
	if a.HealthCondition.Resources == nil {
		a.HealthCondition.Resources = []apphealth.ResourceAssessment{}
	}
	if a.HealthCondition.Warnings == nil {
		a.HealthCondition.Warnings = []string{}
	}
	if a.TargetHelmValuesFiles == nil {
		a.TargetHelmValuesFiles = []string{}
	}
	if a.NamespaceManifestPaths == nil {
		a.NamespaceManifestPaths = map[string]string{}
	}
	if a.NamespaceHelmValues == nil {
		a.NamespaceHelmValues = map[string]core.HelmValuesOverride{}
	}
	if err == nil && len(rawApprovalOverride) > 0 && string(rawApprovalOverride) != "null" {
		var override ApprovalPolicyOverride
		err = json.Unmarshal(rawApprovalOverride, &override)
		if err == nil {
			a.ApprovalPolicyOverride = &override
		}
	}
	if err == nil && len(rawRollbackState) > 0 && string(rawRollbackState) != "null" {
		var rollbackState ApplicationRollbackState
		err = json.Unmarshal(rawRollbackState, &rollbackState)
		if err == nil {
			a.RollbackResumeState = &rollbackState
			a.RollbackResumeAvailable = true
		}
	}
	a.RetryPolicy = NormalizeRetryPolicy(a.RetryPolicy)
	return a, err
}

const applicationColumns = `id,workspace_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,helm_values_files,helm_values_yaml,COALESCE(application_group_id,''),target_manifest_path,namespace_manifest_paths,target_helm_values_files,target_helm_values_yaml,namespace_helm_values,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,COALESCE(last_synced_revision,''),health,status_issues,decommissioning,approval_policy_override,auto_sync_paused,rollback_resume_state,rollback_resume_requires_revision,created_at,retry_enabled,retry_max_attempts,retry_initial_delay_seconds,retry_max_delay_seconds,retry_jitter_percent,retry_attempt_count,retry_next_at,retry_terminal_reason,retry_last_error_code,health_condition_status,health_condition_reason,health_condition_message,health_condition_last_transition_at,health_condition_observed_at,health_condition_resources,health_condition_warnings,COALESCE(repository_configuration_id,''),configuration_path,configuration_commit,configuration_hash,configuration_missing,helm_release_name`

func (s *Store) ApplicationByID(ctx context.Context, id string) (Application, error) {
	return scanApplication(s.DB.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1`, id))
}

func (s *Store) UpdateApplication(ctx context.Context, app Application) error {
	app.RetryPolicy = NormalizeRetryPolicy(app.RetryPolicy)
	if err := app.RetryPolicy.Validate(); err != nil {
		return err
	}
	return s.withApplicationUpdate(ctx, app)
}

func (s *Store) withApplicationUpdate(ctx context.Context, app Application) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := updateApplication(ctx, tx, app); err != nil {
		return err
	}
	return tx.Commit()
}

func updateApplication(ctx context.Context, tx *sql.Tx, app Application) error {
	namespaces, err := json.Marshal(app.Namespaces)
	if err != nil {
		return err
	}
	valuesFiles, err := json.Marshal(app.HelmValuesFiles)
	if err != nil {
		return err
	}
	targetValuesFiles, err := json.Marshal(app.TargetHelmValuesFiles)
	if err != nil {
		return err
	}
	namespaceManifestPaths, err := json.Marshal(app.NamespaceManifestPaths)
	if err != nil {
		return err
	}
	if app.NamespaceManifestPaths == nil {
		namespaceManifestPaths = []byte(`{}`)
	}
	namespaceValues, err := json.Marshal(app.NamespaceHelmValues)
	if err != nil {
		return err
	}
	if app.NamespaceHelmValues == nil {
		namespaceValues = []byte(`{}`)
	}
	var oldCluster, oldSource string
	var oldNamespaces []byte
	if err := tx.QueryRowContext(ctx, `SELECT cluster_id,source_id,namespaces FROM applications WHERE id=$1 FOR UPDATE`, app.ID).Scan(&oldCluster, &oldSource, &oldNamespaces); err != nil {
		return err
	}
	if oldSource != app.SourceID || oldCluster != app.ClusterID {
		var previewSlots int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM preview_slots p JOIN source_control_connections c ON c.id=p.connection_id WHERE c.application_id=$1`, app.ID).Scan(&previewSlots); err != nil {
			return err
		}
		if previewSlots > 0 {
			return errors.New("cannot change Git source or cluster while pull request previews are active")
		}
	}
	var managed, active int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM managed_resources WHERE application_id=$1), (SELECT COUNT(*) FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, app.ID).Scan(&managed, &active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("application is currently syncing")
	}
	var decommissioning bool
	if err := tx.QueryRowContext(ctx, `SELECT decommissioning FROM applications WHERE id=$1`, app.ID).Scan(&decommissioning); err != nil {
		return err
	}
	if decommissioning {
		return errors.New("application is being decommissioned; cancel deletion first")
	}
	var previousBindings []NamespaceBinding
	if err := json.Unmarshal(oldNamespaces, &previousBindings); err != nil {
		return err
	}
	if managed > 0 && (oldCluster != app.ClusterID || !reflect.DeepEqual(previousBindings, app.Namespaces)) {
		return errors.New("cannot change cluster or namespace bindings while resources are managed")
	}
	if app.ApplicationGroupID != "" {
		var duplicateClusterTarget int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications WHERE application_group_id=$1 AND cluster_id=$2 AND id<>$3`, app.ApplicationGroupID, app.ClusterID, app.ID).Scan(&duplicateClusterTarget); err != nil {
			return err
		}
		if duplicateClusterTarget > 0 {
			return errors.New("a deployment group can have only one application per cluster")
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET name=$2,source_id=$3,revision=$4,manifest_path=$5,renderer=$6,kustomize_helm_enabled=$7,kustomize_namespace_override=$8,helm_values_files=$9,helm_values_yaml=$10,target_manifest_path=$11,namespace_manifest_paths=$12,target_helm_values_files=$13,target_helm_values_yaml=$14,namespace_helm_values=$15,cluster_id=$16,namespaces=$17,sync_policy=$18,poll_seconds=$19,retry_enabled=$20,retry_max_attempts=$21,retry_initial_delay_seconds=$22,retry_max_delay_seconds=$23,retry_jitter_percent=$24,retry_attempt_count=0,retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE id=$1`, app.ID, app.Name, app.SourceID, app.Revision, app.ManifestPath, app.Renderer, app.KustomizeHelmEnabled, app.KustomizeNamespaceOverride, valuesFiles, app.HelmValuesYAML, app.TargetManifestPath, namespaceManifestPaths, targetValuesFiles, app.TargetHelmValuesYAML, namespaceValues, app.ClusterID, namespaces, app.SyncPolicy, app.PollSeconds, app.RetryPolicy.Enabled, app.RetryPolicy.MaxAttempts, app.RetryPolicy.InitialDelaySeconds, app.RetryPolicy.MaxDelaySeconds, app.RetryPolicy.JitterPercent); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, app.ID); err != nil {
		return err
	}
	return nil
}

func (s *Store) UpdateApplicationApprovalPolicyOverride(ctx context.Context, id string, override *ApprovalPolicyOverride) error {
	var encoded any
	if override != nil {
		value, err := json.Marshal(override)
		if err != nil {
			return err
		}
		encoded = value
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET approval_policy_override=$2,updated_at=NOW() WHERE id=$1`, id, encoded); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EffectiveApprovalPolicy(ctx context.Context, app Application) (ApprovalPolicy, error) {
	policy := DefaultApprovalPolicy()
	var rawPolicy []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT approval_policy FROM workspaces WHERE id=$1`, app.WorkspaceID).Scan(&rawPolicy); err != nil {
		return ApprovalPolicy{}, err
	}
	if err := json.Unmarshal(rawPolicy, &policy); err != nil {
		return ApprovalPolicy{}, err
	}
	if app.ApprovalPolicyOverride != nil {
		if app.ApprovalPolicyOverride.Sync != nil {
			policy.Sync = *app.ApprovalPolicyOverride.Sync
		}
		if app.ApprovalPolicyOverride.Deletion != nil {
			policy.Deletion = *app.ApprovalPolicyOverride.Deletion
		}
	}
	return policy, nil
}

func (s *Store) DeleteApplicationKeepingResources(ctx context.Context, id string, requireEmpty bool) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return 0, err
	}
	var managed, active int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM managed_resources WHERE application_id=$1), (SELECT COUNT(*) FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&managed, &active); err != nil {
		return 0, err
	}
	if active > 0 {
		return 0, errors.New("application is currently syncing")
	}
	var previewSlots int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM preview_slots p JOIN source_control_connections c ON c.id=p.connection_id WHERE c.application_id=$1`, id).Scan(&previewSlots); err != nil {
		return 0, err
	}
	if previewSlots > 0 {
		return 0, errors.New("application has active pull request previews")
	}
	if requireEmpty && managed > 0 {
		return 0, errors.New("application still manages Kubernetes resources")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return managed, nil
}

func (s *Store) SetApplicationRenderSettings(ctx context.Context, id string, helmEnabled, namespaceOverride bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET kustomize_helm_enabled=$2,kustomize_namespace_override=$3,updated_at=NOW() WHERE id=$1`, id, helmEnabled, namespaceOverride); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelApplicationDecommission(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET decommissioning=FALSE,updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ListApplications(ctx context.Context, workspaceID string) ([]Application, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE workspace_id=$1 ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Application, 0)
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreateApplicationGroup(ctx context.Context, group ApplicationGroup, apps []Application) error {
	valuesFiles, err := json.Marshal(group.HelmValuesFiles)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_groups(id,workspace_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,helm_values_files,helm_values_yaml,sync_policy,poll_seconds) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, group.ID, group.WorkspaceID, group.Name, group.SourceID, group.Revision, group.ManifestPath, group.Renderer, group.KustomizeHelmEnabled, group.KustomizeNamespaceOverride, valuesFiles, group.HelmValuesYAML, group.SyncPolicy, group.PollSeconds); err != nil {
		return err
	}
	for _, app := range apps {
		namespaces, err := json.Marshal(app.Namespaces)
		if err != nil {
			return err
		}
		targetFiles, err := json.Marshal(app.TargetHelmValuesFiles)
		if err != nil {
			return err
		}
		namespaceManifestPaths, err := json.Marshal(app.NamespaceManifestPaths)
		if err != nil {
			return err
		}
		if app.NamespaceManifestPaths == nil {
			namespaceManifestPaths = []byte(`{}`)
		}
		namespaceValues, err := json.Marshal(app.NamespaceHelmValues)
		if err != nil {
			return err
		}
		if app.NamespaceHelmValues == nil {
			namespaceValues = []byte(`{}`)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,helm_values_files,helm_values_yaml,application_group_id,target_manifest_path,namespace_manifest_paths,target_helm_values_files,target_helm_values_yaml,namespace_helm_values,cluster_id,namespaces,sync_policy,poll_seconds) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, app.ID, group.WorkspaceID, app.Name, group.SourceID, group.Revision, group.ManifestPath, group.Renderer, group.KustomizeHelmEnabled, group.KustomizeNamespaceOverride, valuesFiles, group.HelmValuesYAML, group.ID, app.TargetManifestPath, namespaceManifestPaths, targetFiles, app.TargetHelmValuesYAML, namespaceValues, app.ClusterID, namespaces, group.SyncPolicy, group.PollSeconds); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ApplicationGroupByID(ctx context.Context, id string) (ApplicationGroup, error) {
	var group ApplicationGroup
	var valuesFiles []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,workspace_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,helm_values_files,helm_values_yaml,sync_policy,poll_seconds,created_at,updated_at FROM application_groups WHERE id=$1`, id).Scan(&group.ID, &group.WorkspaceID, &group.Name, &group.SourceID, &group.Revision, &group.ManifestPath, &group.Renderer, &group.KustomizeHelmEnabled, &group.KustomizeNamespaceOverride, &valuesFiles, &group.HelmValuesYAML, &group.SyncPolicy, &group.PollSeconds, &group.CreatedAt, &group.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(valuesFiles, &group.HelmValuesFiles)
	}
	if group.HelmValuesFiles == nil {
		group.HelmValuesFiles = []string{}
	}
	return group, err
}

func (s *Store) ApplicationsByGroupID(ctx context.Context, id string) ([]Application, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE application_group_id=$1 ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Application{}
	for rows.Next() {
		item, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateApplicationGroup(ctx context.Context, group ApplicationGroup) error {
	valuesFiles, err := json.Marshal(group.HelmValuesFiles)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM application_groups WHERE id=$1 FOR UPDATE`, group.ID).Scan(&lockedID); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications a WHERE a.application_group_id=$1 AND (a.decommissioning OR a.auto_sync_paused OR EXISTS (SELECT 1 FROM operations o WHERE o.application_id=a.id AND o.status IN ('queued','running')))`, group.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("a target is syncing, deleting, or pinned to rollback; shared configuration cannot be changed")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_groups SET source_id=$2,revision=$3,manifest_path=$4,kustomize_helm_enabled=$5,kustomize_namespace_override=$6,helm_values_files=$7,helm_values_yaml=$8,sync_policy=$9,poll_seconds=$10,updated_at=NOW() WHERE id=$1`, group.ID, group.SourceID, group.Revision, group.ManifestPath, group.KustomizeHelmEnabled, group.KustomizeNamespaceOverride, valuesFiles, group.HelmValuesYAML, group.SyncPolicy, group.PollSeconds); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET source_id=$2,revision=$3,manifest_path=$4,kustomize_helm_enabled=$5,kustomize_namespace_override=$6,helm_values_files=$7,helm_values_yaml=$8,sync_policy=$9,poll_seconds=$10,last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE application_group_id=$1`, group.ID, group.SourceID, group.Revision, group.ManifestPath, group.KustomizeHelmEnabled, group.KustomizeNamespaceOverride, valuesFiles, group.HelmValuesYAML, group.SyncPolicy, group.PollSeconds); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE application_group_id=$1)`, group.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DueApplications(ctx context.Context, limit int) ([]Application, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+applicationColumns+`
		FROM applications a
		WHERE NOT a.decommissioning
		  AND NOT a.auto_sync_paused
		  AND NOT a.configuration_missing
		  AND a.retry_terminal_reason=''
		  AND (
		    (a.retry_next_at IS NOT NULL AND a.retry_next_at<=NOW())
		    OR (a.retry_next_at IS NULL AND (a.last_checked_at IS NULL OR a.last_checked_at<=NOW()-(a.poll_seconds * INTERVAL '1 second')))
		    OR (a.retry_next_at IS NULL AND EXISTS (SELECT 1 FROM git_push_triggers t WHERE t.application_id=a.id))
		  )
		  AND NOT EXISTS (SELECT 1 FROM operation_leases l WHERE l.application_id=a.id AND l.expires_at>NOW())
		ORDER BY COALESCE(a.retry_next_at,a.last_checked_at) ASC NULLS FIRST
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Application, 0)
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, app)
	}
	return items, rows.Err()
}
func (s *Store) UpdateApplicationHealth(ctx context.Context, id, health, checkedAt string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE applications SET health=$2,last_checked_at=$3::timestamptz,updated_at=NOW() WHERE id=$1`, id, health, checkedAt)
	return err
}

func (s *Store) RecordApplicationHealthCondition(ctx context.Context, id string, condition apphealth.ApplicationCondition) error {
	if condition.Status == "" {
		condition.Status = apphealth.Unknown
	}
	if condition.Reason == "" {
		condition.Reason = "HealthObservationUnavailable"
	}
	if condition.Message == "" {
		condition.Message = "Kubernetes health could not be determined."
	}
	if condition.Resources == nil {
		condition.Resources = []apphealth.ResourceAssessment{}
	}
	if condition.Warnings == nil {
		condition.Warnings = []string{}
	}
	resources, err := json.Marshal(condition.Resources)
	if err != nil {
		return err
	}
	warnings, err := json.Marshal(condition.Warnings)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var previousStatus, previousReason string
	var previousTransition time.Time
	if err := tx.QueryRowContext(ctx, `SELECT health_condition_status,health_condition_reason,health_condition_last_transition_at FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&previousStatus, &previousReason, &previousTransition); err != nil {
		return err
	}
	now := time.Now().UTC()
	observedAt := now
	if condition.ObservedAt != nil {
		observedAt = condition.ObservedAt.UTC()
	}
	changed := previousStatus != string(condition.Status) || previousReason != condition.Reason
	if changed {
		condition.LastTransitionTime = now
	} else {
		condition.LastTransitionTime = previousTransition.UTC()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET health_condition_status=$2,health_condition_reason=$3,health_condition_message=$4,health_condition_last_transition_at=$5,health_condition_observed_at=$6,health_condition_resources=$7::jsonb,health_condition_warnings=$8::jsonb,updated_at=NOW() WHERE id=$1`, id, condition.Status, condition.Reason, condition.Message, condition.LastTransitionTime, observedAt, resources, warnings); err != nil {
		return err
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_health_transitions(application_id,status,reason,message,resources,changed_at) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, id, condition.Status, condition.Reason, condition.Message, resources, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM application_health_transitions WHERE id IN (SELECT id FROM application_health_transitions WHERE application_id=$1 ORDER BY changed_at DESC,id DESC OFFSET 100)`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ApplicationHealthTransitions(ctx context.Context, applicationID string, limit int) ([]apphealth.Transition, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,status,reason,message,resources,changed_at FROM application_health_transitions WHERE application_id=$1 ORDER BY changed_at DESC,id DESC LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]apphealth.Transition, 0)
	for rows.Next() {
		var item apphealth.Transition
		var rawResources []byte
		if err := rows.Scan(&item.ID, &item.Status, &item.Reason, &item.Message, &rawResources, &item.ChangedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawResources, &item.Resources); err != nil {
			return nil, err
		}
		if item.Resources == nil {
			item.Resources = []apphealth.ResourceAssessment{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SetApplicationStatusIssues(ctx context.Context, id string, issues []ApplicationStatusIssue) error {
	if issues == nil {
		issues = []ApplicationStatusIssue{}
	}
	raw, err := json.Marshal(issues)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE applications SET health='degraded',status_issues=$2::jsonb,last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, id, raw)
	return err
}
func (s *Store) MarkApplicationSynced(ctx context.Context, id, revision, health string) error {
	if revision == "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE applications SET health=$2,status_issues='[]'::jsonb,retry_attempt_count=0,retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, id, health)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE applications SET last_synced_revision=$2,health=$3,status_issues='[]'::jsonb,retry_attempt_count=0,retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, id, revision, health)
	return err
}

func (s *Store) PauseAutoSync(ctx context.Context, id string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,health='degraded',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1 AND sync_policy='auto-safe'`, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return err
	}
	app, err := s.ApplicationByID(ctx, id)
	if err != nil {
		return err
	}
	condition := app.HealthCondition
	if condition.Status == apphealth.Degraded || condition.Status == apphealth.Missing || condition.Status == apphealth.Partial {
		return nil
	}
	condition.Status = apphealth.Suspended
	condition.Reason = "ReconciliationSuspended"
	condition.Message = "Automatic reconciliation is paused; runtime health reflects the last successful Kubernetes observation."
	return s.RecordApplicationHealthCondition(ctx, id, condition)
}

func (s *Store) RecordApplicationRetry(ctx context.Context, id string, attempt int, errorCode string, nextRetryAt *time.Time, terminalReason string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE applications SET retry_attempt_count=$2,retry_last_error_code=$3,retry_next_at=$4,retry_terminal_reason=$5,health='degraded',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, id, attempt, errorCode, nextRetryAt, terminalReason)
	return err
}

func (s *Store) ResetApplicationRetry(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE applications SET retry_attempt_count=0,retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (s *Store) ApplicationHasActiveOperation(ctx context.Context, id string) (bool, error) {
	var active bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1)`, id).Scan(&active)
	return active, err
}

// PinApplicationForRollback switches the saved source/render settings to the
// rollback target and pauses reconciliation. The prior tracked settings remain
// available for an explicit owner-initiated resume.
func (s *Store) PinApplicationForRollback(ctx context.Context, id, planID string, settings core.RollbackSettings, revision string, requireRevision bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	app, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if app.ClusterID != settings.ClusterID || !sameApplicationNamespaces(app.Namespaces, settings.Namespaces) {
		return errors.New("rollback target scope changed before it could be pinned")
	}
	prior := &ApplicationRollbackState{SourceID: app.SourceID, Revision: app.Revision, ManifestPath: app.ManifestPath, TargetManifestPath: app.TargetManifestPath, NamespaceManifestPaths: app.NamespaceManifestPaths, Renderer: app.Renderer, KustomizeHelmEnabled: app.KustomizeHelmEnabled, KustomizeNamespaceOverride: app.KustomizeNamespaceOverride, HelmReleaseName: app.HelmReleaseName, HelmValuesFiles: app.HelmValuesFiles, HelmValuesYAML: app.HelmValuesYAML, TargetHelmValuesFiles: app.TargetHelmValuesFiles, TargetHelmValuesYAML: app.TargetHelmValuesYAML, NamespaceHelmValues: app.NamespaceHelmValues, ClusterID: app.ClusterID, Namespaces: app.Namespaces, SyncPolicy: app.SyncPolicy, PollSeconds: app.PollSeconds}
	rawPrior, err := json.Marshal(prior)
	if err != nil {
		return err
	}
	pinnedRevision := revision
	if pinnedRevision == "" {
		pinnedRevision = settings.Revision
	}
	valuesFiles, err := json.Marshal(settings.HelmValuesFiles)
	if err != nil {
		return err
	}
	namespaceManifestPaths, err := json.Marshal(settings.NamespaceManifestPaths)
	if err != nil {
		return err
	}
	if settings.NamespaceManifestPaths == nil {
		namespaceManifestPaths = []byte(`{}`)
	}
	targetValuesFiles, err := json.Marshal(settings.TargetHelmValuesFiles)
	if err != nil {
		return err
	}
	namespaceValues, err := json.Marshal(settings.NamespaceHelmValues)
	if err != nil {
		return err
	}
	if settings.NamespaceHelmValues == nil {
		namespaceValues = []byte(`{}`)
	}
	// Keep the currently configured cluster and namespace credential bindings.
	// A rollback changes desired application content, never authentication wiring.
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET source_id=$2,revision=$3,manifest_path=$4,target_manifest_path=$5,namespace_manifest_paths=$6,renderer=$7,kustomize_helm_enabled=$8,kustomize_namespace_override=$9,helm_values_files=$10,helm_values_yaml=$11,target_helm_values_files=$12,target_helm_values_yaml=$13,namespace_helm_values=$14,auto_sync_paused=TRUE,rollback_resume_state=$15,rollback_resume_requires_revision=$16,helm_release_name=$17,last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE id=$1`, id, settings.SourceID, pinnedRevision, settings.ManifestPath, settings.TargetManifestPath, namespaceManifestPaths, settings.Renderer, settings.KustomizeHelmEnabled, settings.KustomizeNamespaceOverride, valuesFiles, settings.HelmValuesYAML, targetValuesFiles, settings.TargetHelmValuesYAML, namespaceValues, rawPrior, requireRevision, settings.HelmReleaseName); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current' AND id<>$2`, id, planID); err != nil {
		return err
	}
	return tx.Commit()
}

func sameApplicationNamespaces(bindings []NamespaceBinding, names []string) bool {
	if len(bindings) != len(names) {
		return false
	}
	actual := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		actual = append(actual, binding.Namespace)
	}
	sort.Strings(actual)
	expected := append([]string(nil), names...)
	sort.Strings(expected)
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func (s *Store) KeepRollbackPin(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET rollback_resume_state=NULL,rollback_resume_requires_revision=FALSE,auto_sync_paused=TRUE,last_checked_at=NULL,updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResumeRollbackTracking(ctx context.Context, id, selectedRevision string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	app, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing")
	}
	if app.RollbackResumeRequiresRevision && selectedRevision == "" {
		return errors.New("select and verify a Git revision before resuming this first deployment")
	}
	if state := app.RollbackResumeState; state != nil {
		if selectedRevision != "" {
			state.Revision = selectedRevision
		}
		namespaces, err := json.Marshal(state.Namespaces)
		if err != nil {
			return err
		}
		valuesFiles, err := json.Marshal(state.HelmValuesFiles)
		if err != nil {
			return err
		}
		namespaceManifestPaths, err := json.Marshal(state.NamespaceManifestPaths)
		if err != nil {
			return err
		}
		if state.NamespaceManifestPaths == nil {
			namespaceManifestPaths = []byte(`{}`)
		}
		targetValuesFiles, err := json.Marshal(state.TargetHelmValuesFiles)
		if err != nil {
			return err
		}
		namespaceValues, err := json.Marshal(state.NamespaceHelmValues)
		if err != nil {
			return err
		}
		if state.NamespaceHelmValues == nil {
			namespaceValues = []byte(`{}`)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET source_id=$2,revision=$3,manifest_path=$4,target_manifest_path=$5,namespace_manifest_paths=$6,renderer=$7,kustomize_helm_enabled=$8,kustomize_namespace_override=$9,helm_values_files=$10,helm_values_yaml=$11,target_helm_values_files=$12,target_helm_values_yaml=$13,namespace_helm_values=$14,cluster_id=$15,namespaces=$16,sync_policy=$17,poll_seconds=$18,helm_release_name=$19,auto_sync_paused=FALSE,rollback_resume_state=NULL,rollback_resume_requires_revision=FALSE,last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE id=$1`, id, state.SourceID, state.Revision, state.ManifestPath, state.TargetManifestPath, namespaceManifestPaths, state.Renderer, state.KustomizeHelmEnabled, state.KustomizeNamespaceOverride, valuesFiles, state.HelmValuesYAML, targetValuesFiles, state.TargetHelmValuesYAML, namespaceValues, state.ClusterID, namespaces, state.SyncPolicy, state.PollSeconds, state.HelmReleaseName); err != nil {
			return err
		}
	} else {
		if selectedRevision == "" {
			_, err = tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=FALSE,last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE id=$1`, id)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE applications SET revision=$2,auto_sync_paused=FALSE,last_checked_at=NULL,health='unknown',status_issues='[]'::jsonb,updated_at=NOW() WHERE id=$1`, id, selectedRevision)
		}
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RenewOperationLease(ctx context.Context, operationID string, lease time.Duration) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE operation_leases SET expires_at=NOW()+($2 * INTERVAL '1 second') WHERE operation_id=$1`, operationID, int64(lease.Seconds()))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("sync operation lost its database lease")
	}
	return nil
}

func (s *Store) Audit(ctx context.Context, actorID, action, resourceType, resourceID string, details any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES(NULLIF($1,''),$2,$3,$4,$5)`, actorID, action, resourceType, resourceID, payload)
	return err
}

type PlanRecord struct {
	ID        string          `json:"id"`
	Plan      core.Plan       `json:"plan"`
	Trigger   *GitPushTrigger `json:"trigger,omitempty"`
	Desired   []core.Resource `json:"desired"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	Status    string          `json:"status"`
}

type RollbackSnapshot struct {
	ID            string
	ApplicationID string
	OperationID   string
	Kind          string
	Revision      string
	ResourceCount int
	PayloadCipher []byte
	CreatedAt     time.Time
}

type RollbackTargetSummary struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	OperationID   string    `json:"operationId,omitempty"`
	Revision      string    `json:"revision,omitempty"`
	ResourceCount int       `json:"resourceCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (s *Store) SaveRollbackSnapshot(ctx context.Context, snapshot RollbackSnapshot) error {
	if snapshot.ID == "" || snapshot.ApplicationID == "" || len(snapshot.PayloadCipher) == 0 || (snapshot.Kind != "successful_sync" && snapshot.Kind != "pre_operation") {
		return errors.New("rollback snapshot is incomplete")
	}
	var operation any
	if snapshot.OperationID != "" {
		operation = snapshot.OperationID
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rollback_snapshots(id,application_id,operation_id,kind,revision,resource_count,payload_cipher) VALUES($1,$2,$3,$4,$5,$6,$7)`, snapshot.ID, snapshot.ApplicationID, operation, snapshot.Kind, snapshot.Revision, snapshot.ResourceCount, snapshot.PayloadCipher); err != nil {
		return err
	}
	if snapshot.Kind == "pre_operation" && snapshot.OperationID != "" {
		result, err := tx.ExecContext(ctx, `UPDATE operations SET rollback_checkpoint_id=$2 WHERE id=$1 AND application_id=$3`, snapshot.OperationID, snapshot.ID, snapshot.ApplicationID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("pre-operation checkpoint has no matching operation")
		}
	}
	return tx.Commit()
}

func (s *Store) RollbackSnapshotByID(ctx context.Context, applicationID, id string) (RollbackSnapshot, error) {
	var snapshot RollbackSnapshot
	err := s.DB.QueryRowContext(ctx, `SELECT id,application_id,COALESCE(operation_id,''),kind,revision,resource_count,payload_cipher,created_at FROM rollback_snapshots WHERE application_id=$1 AND id=$2`, applicationID, id).Scan(&snapshot.ID, &snapshot.ApplicationID, &snapshot.OperationID, &snapshot.Kind, &snapshot.Revision, &snapshot.ResourceCount, &snapshot.PayloadCipher, &snapshot.CreatedAt)
	return snapshot, err
}

func (s *Store) ListRollbackTargets(ctx context.Context, applicationID string) ([]RollbackTargetSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.kind,COALESCE(r.operation_id,''),r.revision,r.resource_count,r.created_at FROM rollback_snapshots r LEFT JOIN operations o ON o.id=r.operation_id WHERE r.application_id=$1 AND ((r.kind='successful_sync' AND o.status='succeeded') OR (r.kind='pre_operation' AND o.status='failed' AND o.rollback_checkpoint_id=r.id)) ORDER BY r.created_at DESC,r.id`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RollbackTargetSummary, 0)
	for rows.Next() {
		var item RollbackTargetSummary
		if err := rows.Scan(&item.ID, &item.Kind, &item.OperationID, &item.Revision, &item.ResourceCount, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SavePlan(ctx context.Context, record PlanRecord) error {
	bindings, err := json.Marshal(record.Plan.Bindings)
	if err != nil {
		return err
	}
	changes, err := json.Marshal(record.Plan.Changes)
	if err != nil {
		return err
	}
	ignored, err := json.Marshal(record.Plan.Ignored)
	if err != nil {
		return err
	}
	selection, err := json.Marshal(record.Plan.Selection)
	if err != nil {
		return err
	}
	desired, err := json.Marshal(record.Desired)
	if err != nil {
		return err
	}
	approverRoles, err := json.Marshal(record.Plan.ApproverRoles)
	if err != nil {
		return err
	}
	approverUserIDs, err := json.Marshal(record.Plan.ApproverUserIDs)
	if err != nil {
		return err
	}
	var rollbackTarget any
	if record.Plan.Rollback != nil {
		encoded, err := json.Marshal(record.Plan.Rollback)
		if err != nil {
			return err
		}
		rollbackTarget = encoded
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applicationID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, record.Plan.ApplicationID).Scan(&applicationID); err != nil {
		return err
	}
	var operationActive bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1 AND expires_at>NOW())`, record.Plan.ApplicationID).Scan(&operationActive); err != nil {
		return err
	}
	if operationActive {
		return errors.New("application is currently syncing; plan refresh is temporarily unavailable")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, record.Plan.ApplicationID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,bindings,changes,desired,created_by,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission,approval_kind,required_approvals,approver_roles,approver_user_ids,rollback_target) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, record.ID, record.Plan.ApplicationID, record.Plan.Revision, record.Plan.Digest, bindings, changes, desired, record.CreatedBy, record.ExpiresAt, record.Status, ignored, selection, record.Plan.IgnoreRulesDigest, record.Plan.Decommission, record.Plan.ApprovalKind, record.Plan.RequiredApprovals, approverRoles, approverUserIDs, rollbackTarget)
	if err != nil {
		return err
	}
	if record.Plan.Decommission {
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET decommissioning=TRUE,updated_at=NOW() WHERE id=$1`, record.Plan.ApplicationID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) PlanByID(ctx context.Context, id string) (PlanRecord, error) {
	var out PlanRecord
	var bindings, changes, desired, ignored, selection, approverRoles, approverUserIDs, rollbackTarget, triggerInfo []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,application_id,revision,digest,bindings,changes,desired,created_by,created_at,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission,approval_kind,required_approvals,approver_roles,approver_user_ids,rollback_target,trigger_info FROM plans WHERE id=$1`, id).Scan(&out.ID, &out.Plan.ApplicationID, &out.Plan.Revision, &out.Plan.Digest, &bindings, &changes, &desired, &out.CreatedBy, &out.CreatedAt, &out.ExpiresAt, &out.Status, &ignored, &selection, &out.Plan.IgnoreRulesDigest, &out.Plan.Decommission, &out.Plan.ApprovalKind, &out.Plan.RequiredApprovals, &approverRoles, &approverUserIDs, &rollbackTarget, &triggerInfo)
	if err != nil {
		return PlanRecord{}, err
	}
	if len(triggerInfo) > 0 {
		out.Trigger = &GitPushTrigger{}
		if err = json.Unmarshal(triggerInfo, out.Trigger); err != nil {
			return PlanRecord{}, err
		}
	}
	if err = json.Unmarshal(bindings, &out.Plan.Bindings); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(changes, &out.Plan.Changes); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(ignored, &out.Plan.Ignored); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(selection, &out.Plan.Selection); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(desired, &out.Desired); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(approverRoles, &out.Plan.ApproverRoles); err != nil {
		return PlanRecord{}, err
	}
	if err = json.Unmarshal(approverUserIDs, &out.Plan.ApproverUserIDs); err != nil {
		return PlanRecord{}, err
	}
	if len(rollbackTarget) > 0 && string(rollbackTarget) != "null" {
		var target core.RollbackTarget
		if err = json.Unmarshal(rollbackTarget, &target); err != nil {
			return PlanRecord{}, err
		}
		out.Plan.Rollback = &target
	}
	out.Plan.RequiresApproval = core.RequiredApprovalCount(out.Plan) > 0
	for _, change := range out.Plan.Changes {
		if change.Kind == core.Delete || change.Identity.ClusterScoped {
			out.Plan.RequiresApproval = true
			break
		}
	}
	return out, nil
}

func (s *Store) ListPlans(ctx context.Context, applicationID string, limit int) ([]PlanRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,application_id,revision,digest,bindings,changes,desired,created_by,created_at,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission,approval_kind,required_approvals,approver_roles,approver_user_ids,rollback_target,trigger_info FROM plans WHERE application_id=$1 ORDER BY created_at DESC LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlanRecord, 0)
	for rows.Next() {
		var out PlanRecord
		var bindings, changes, desired, ignored, selection, approverRoles, approverUserIDs, rollbackTarget, triggerInfo []byte
		if err := rows.Scan(&out.ID, &out.Plan.ApplicationID, &out.Plan.Revision, &out.Plan.Digest, &bindings, &changes, &desired, &out.CreatedBy, &out.CreatedAt, &out.ExpiresAt, &out.Status, &ignored, &selection, &out.Plan.IgnoreRulesDigest, &out.Plan.Decommission, &out.Plan.ApprovalKind, &out.Plan.RequiredApprovals, &approverRoles, &approverUserIDs, &rollbackTarget, &triggerInfo); err != nil {
			return nil, err
		}
		if len(triggerInfo) > 0 {
			out.Trigger = &GitPushTrigger{}
			if err := json.Unmarshal(triggerInfo, out.Trigger); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(bindings, &out.Plan.Bindings); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(changes, &out.Plan.Changes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ignored, &out.Plan.Ignored); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(selection, &out.Plan.Selection); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(approverRoles, &out.Plan.ApproverRoles); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(approverUserIDs, &out.Plan.ApproverUserIDs); err != nil {
			return nil, err
		}
		if len(rollbackTarget) > 0 && string(rollbackTarget) != "null" {
			var target core.RollbackTarget
			if err := json.Unmarshal(rollbackTarget, &target); err != nil {
				return nil, err
			}
			out.Plan.Rollback = &target
		}
		if err := json.Unmarshal(desired, &out.Desired); err != nil {
			return nil, err
		}
		out.Plan.RequiresApproval = core.RequiredApprovalCount(out.Plan) > 0
		for _, change := range out.Plan.Changes {
			if change.Kind == core.Delete || change.Identity.ClusterScoped {
				out.Plan.RequiresApproval = true
				break
			}
		}
		items = append(items, out)
	}
	return items, rows.Err()
}

type ApplicationIgnoreRule struct {
	core.IgnoreRule
	ApplicationID string    `json:"applicationId"`
	CreatedBy     string    `json:"createdBy"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (s *Store) IgnoreRules(ctx context.Context, applicationID string) ([]ApplicationIgnoreRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,cluster_id,api_version,kind,namespace,name,path,reason,application_id,created_by,created_at,managed_by_git FROM application_ignore_rules WHERE application_id=$1 ORDER BY created_at,id`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ApplicationIgnoreRule, 0)
	for rows.Next() {
		var item ApplicationIgnoreRule
		if err := rows.Scan(&item.ID, &item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.Path, &item.Reason, &item.ApplicationID, &item.CreatedBy, &item.CreatedAt, &item.ManagedByGit); err != nil {
			return nil, err
		}
		item.Identity.ClusterScoped = item.Identity.Namespace == ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateIgnoreRule(ctx context.Context, applicationID, actorID string, rule core.IgnoreRule) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&locked); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1 AND expires_at>NOW())`, applicationID).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing; ignore rules cannot change")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_ignore_rules(id,application_id,cluster_id,api_version,kind,namespace,name,path,reason,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, rule.ID, applicationID, rule.Identity.ClusterID, rule.Identity.APIVersion, rule.Identity.Kind, rule.Identity.Namespace, rule.Identity.Name, rule.Path, rule.Reason, actorID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, applicationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteIgnoreRule(ctx context.Context, applicationID, id string) (ApplicationIgnoreRule, error) {
	var item ApplicationIgnoreRule
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&locked); err != nil {
		return item, err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1 AND expires_at>NOW())`, applicationID).Scan(&active); err != nil {
		return item, err
	}
	if active {
		return item, errors.New("application is currently syncing; ignore rules cannot change")
	}
	var managedByGit bool
	if err := tx.QueryRowContext(ctx, `SELECT managed_by_git FROM application_ignore_rules WHERE application_id=$1 AND id=$2`, applicationID, id).Scan(&managedByGit); err != nil {
		return item, err
	}
	if managedByGit {
		return item, ErrGitManagedIgnore
	}
	err = tx.QueryRowContext(ctx, `DELETE FROM application_ignore_rules WHERE application_id=$1 AND id=$2 RETURNING id,cluster_id,api_version,kind,namespace,name,path,reason,application_id,created_by,created_at,managed_by_git`, applicationID, id).Scan(&item.ID, &item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.Path, &item.Reason, &item.ApplicationID, &item.CreatedBy, &item.CreatedAt, &item.ManagedByGit)
	if err != nil {
		return item, err
	}
	item.Identity.ClusterScoped = item.Identity.Namespace == ""
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, applicationID); err != nil {
		return item, err
	}
	return item, tx.Commit()
}

func (s *Store) IgnoreSelectors(ctx context.Context, applicationID string) ([]core.IgnoreSelector, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,api_version,kind,label_key,label_value,reason,created_by,created_at,managed_by_git FROM application_ignore_selectors WHERE application_id=$1 ORDER BY created_at,id`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]core.IgnoreSelector, 0)
	for rows.Next() {
		var item core.IgnoreSelector
		if err := rows.Scan(&item.ID, &item.APIVersion, &item.Kind, &item.LabelKey, &item.LabelValue, &item.Reason, &item.CreatedBy, &item.CreatedAt, &item.ManagedByGit); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ChangeIgnoreSelector(ctx context.Context, applicationID, actorID string, rule core.IgnoreSelector, deleteID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&locked); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1 AND expires_at>NOW())`, applicationID).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing; ignore rules cannot change")
	}
	if deleteID != "" {
		var managedByGit bool
		if err := tx.QueryRowContext(ctx, `SELECT managed_by_git FROM application_ignore_selectors WHERE application_id=$1 AND id=$2`, applicationID, deleteID).Scan(&managedByGit); err != nil {
			return err
		}
		if managedByGit {
			return ErrGitManagedIgnore
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM application_ignore_selectors WHERE id=$1 AND application_id=$2`, deleteID, applicationID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_ignore_selectors(id,application_id,api_version,kind,label_key,label_value,reason,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, rule.ID, applicationID, rule.APIVersion, rule.Kind, rule.LabelKey, rule.LabelValue, rule.Reason, actorID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, applicationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPlanStatus(ctx context.Context, id, status string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE plans SET status=$2 WHERE id=$1`, id, status)
	return err
}

func (s *Store) CreateApproval(ctx context.Context, id, planID string, approval core.DeletionApproval, expires time.Time, comment string) error {
	deletes, err := json.Marshal(approval.Deletes)
	if err != nil {
		return err
	}
	privileged, err := json.Marshal(approval.Privileged)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Deletes    json.RawMessage `json:"deletes"`
		Privileged json.RawMessage `json:"privileged"`
	}{Deletes: deletes, Privileged: privileged})
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, digest string
	var workspaceID string
	var planExpires time.Time
	var required int
	var changesRaw, approverRolesRaw, approverUserIDsRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT p.status,p.digest,p.expires_at,p.required_approvals,p.changes,p.approver_roles,p.approver_user_ids,a.workspace_id FROM plans p JOIN applications a ON a.id=p.application_id WHERE p.id=$1 FOR UPDATE OF p`, planID).Scan(&status, &digest, &planExpires, &required, &changesRaw, &approverRolesRaw, &approverUserIDsRaw, &workspaceID); err != nil {
		return err
	}
	if status != "current" || digest != approval.PlanDigest || !time.Now().Before(planExpires) {
		return errors.New("plan is no longer current or has expired")
	}
	var operationActive bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE plan_id=$1 AND status IN ('queued','running'))`, planID).Scan(&operationActive); err != nil {
		return err
	}
	if operationActive {
		return errors.New("application already has an active operation")
	}
	var duplicate bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM deletion_approvals WHERE plan_id=$1 AND plan_digest=$2 AND actor_id=$3 AND used_at IS NULL AND expires_at>NOW())`, planID, approval.PlanDigest, approval.ActorID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return errors.New("member has already approved this plan")
	}
	if required == 0 {
		var changes []core.Change
		if err := json.Unmarshal(changesRaw, &changes); err != nil {
			return err
		}
		for _, change := range changes {
			if change.Kind == core.Delete || change.Identity.ClusterScoped {
				required = 1
				break
			}
		}
	}
	var rule ApprovalRule
	if err := json.Unmarshal(approverRolesRaw, &rule.ApproverRoles); err != nil {
		return err
	}
	if err := json.Unmarshal(approverUserIDsRaw, &rule.ApproverUserIDs); err != nil {
		return err
	}
	rule.RequiredApprovals = required
	workspaceRole := func(actorID string) (string, error) {
		var role string
		err := tx.QueryRowContext(ctx, `SELECT CASE WHEN u.is_admin THEN 'owner' ELSE COALESCE((SELECT member_roles.role FROM (
			SELECT role FROM workspace_memberships WHERE workspace_id=$1 AND user_id=u.id
			UNION ALL SELECT role FROM oidc_membership_grants WHERE workspace_id=$1 AND user_id=u.id
		) member_roles ORDER BY CASE member_roles.role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END DESC LIMIT 1),'') END FROM users u WHERE u.id=$2 AND u.disabled=FALSE AND u.deleted_at IS NULL`, workspaceID, actorID).Scan(&role)
		return role, err
	}
	currentRole, err := workspaceRole(approval.ActorID)
	if err != nil || currentRole == "" || !ApprovalRuleAllows(rule, currentRole, approval.ActorID) {
		return errors.New("approver is no longer eligible for this plan")
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT actor_id FROM deletion_approvals WHERE plan_id=$1 AND plan_digest=$2 AND used_at IS NULL AND expires_at>NOW()`, planID, approval.PlanDigest)
	if err != nil {
		return err
	}
	actors := make([]string, 0)
	for rows.Next() {
		var actorID string
		if err := rows.Scan(&actorID); err != nil {
			rows.Close()
			return err
		}
		actors = append(actors, actorID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	eligibleApprovals := 0
	for _, actorID := range actors {
		role, err := workspaceRole(actorID)
		if err != nil {
			return err
		}
		if role != "" && ApprovalRuleAllows(rule, role, actorID) {
			eligibleApprovals++
		}
	}
	if required == 0 || eligibleApprovals >= required {
		return errors.New("this plan already has all required approvals")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO deletion_approvals(id,plan_id,actor_id,plan_digest,deletes,expires_at,comment) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, planID, approval.ActorID, approval.PlanDigest, payload, expires, comment); err != nil {
		return err
	}
	return tx.Commit()
}

type ApprovalRecord struct {
	Comment   string
	ID        string
	PlanID    string
	Approval  core.DeletionApproval
	UsedAt    *time.Time
	CreatedAt time.Time
}

func (s *Store) ApprovalByID(ctx context.Context, id string) (ApprovalRecord, error) {
	var out ApprovalRecord
	var payload []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,plan_id,actor_id,plan_digest,deletes,expires_at,used_at,created_at,comment FROM deletion_approvals WHERE id=$1`, id).Scan(&out.ID, &out.PlanID, &out.Approval.ActorID, &out.Approval.PlanDigest, &payload, &out.Approval.ExpiresAt, &out.UsedAt, &out.CreatedAt, &out.Comment)
	if err != nil {
		return ApprovalRecord{}, err
	}
	var values struct {
		Deletes    []core.Change `json:"deletes"`
		Privileged []core.Change `json:"privileged"`
	}
	if err := json.Unmarshal(payload, &values); err != nil {
		return ApprovalRecord{}, err
	}
	out.Approval.Deletes = values.Deletes
	out.Approval.Privileged = values.Privileged
	return out, nil
}

func (s *Store) ListPlanApprovals(ctx context.Context, planID, digest string) ([]ApprovalRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,plan_id,actor_id,plan_digest,deletes,expires_at,used_at,created_at,comment FROM deletion_approvals WHERE plan_id=$1 AND plan_digest=$2 AND used_at IS NULL AND expires_at>NOW() ORDER BY created_at,id`, planID, digest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ApprovalRecord, 0)
	for rows.Next() {
		var item ApprovalRecord
		var payload []byte
		if err := rows.Scan(&item.ID, &item.PlanID, &item.Approval.ActorID, &item.Approval.PlanDigest, &payload, &item.Approval.ExpiresAt, &item.UsedAt, &item.CreatedAt, &item.Comment); err != nil {
			return nil, err
		}
		var values struct {
			Deletes    []core.Change `json:"deletes"`
			Privileged []core.Change `json:"privileged"`
		}
		if err := json.Unmarshal(payload, &values); err != nil {
			return nil, err
		}
		item.Approval.Deletes = values.Deletes
		item.Approval.Privileged = values.Privileged
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ConsumeApproval(ctx context.Context, id, planID, digest string) (bool, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE deletion_approvals SET used_at=NOW() WHERE id=$1 AND plan_id=$2 AND plan_digest=$3 AND used_at IS NULL AND expires_at>NOW()`, id, planID, digest)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

type ManagedResource struct {
	Identity        core.Identity
	UID             string
	ResourceVersion string
	Manifest        json.RawMessage
	Adopted         bool
}

func (s *Store) ManagedResources(ctx context.Context, applicationID string) ([]ManagedResource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest,adopted FROM managed_resources WHERE application_id=$1 ORDER BY api_version,kind,namespace,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ManagedResource, 0)
	for rows.Next() {
		var item ManagedResource
		if err := rows.Scan(&item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.UID, &item.ResourceVersion, &item.Manifest, &item.Adopted); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type ObservedResource struct {
	Identity        core.Identity             `json:"identity"`
	UID             string                    `json:"uid"`
	ResourceVersion string                    `json:"resourceVersion"`
	Labels          map[string]string         `json:"labels"`
	OwnerUIDs       []string                  `json:"ownerUids"`
	Phase           string                    `json:"phase,omitempty"`
	Readiness       string                    `json:"readiness,omitempty"`
	HealthSummary   apphealth.ResourceDetails `json:"healthSummary,omitempty"`
	Source          string                    `json:"source"`
	ObservedAt      time.Time                 `json:"observedAt"`
}

func (s *Store) ObservedResources(ctx context.Context, applicationID string) ([]ObservedResource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cluster_id,api_version,kind,namespace,name,uid,resource_version,labels,owner_uids,phase,readiness,source,observed_at,health_summary FROM application_resource_observations WHERE application_id=$1 ORDER BY kind,namespace,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ObservedResource{}
	for rows.Next() {
		var item ObservedResource
		var labels, owners, healthSummary []byte
		if err := rows.Scan(&item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.UID, &item.ResourceVersion, &labels, &owners, &item.Phase, &item.Readiness, &item.Source, &item.ObservedAt, &healthSummary); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(labels, &item.Labels); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(owners, &item.OwnerUIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(healthSummary, &item.HealthSummary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceObservedResources(ctx context.Context, applicationID string, items []ObservedResource) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_resource_observations WHERE application_id=$1 AND source='kubernetes'`, applicationID); err != nil {
		return err
	}
	for _, item := range items {
		labels, err := json.Marshal(item.Labels)
		if err != nil {
			return err
		}
		owners, err := json.Marshal(item.OwnerUIDs)
		if err != nil {
			return err
		}
		healthSummary, err := json.Marshal(item.HealthSummary)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_resource_observations(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,labels,owner_uids,phase,readiness,source,observed_at,health_summary) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'kubernetes',NOW(),$13) ON CONFLICT(application_id,cluster_id,api_version,kind,namespace,name) DO UPDATE SET uid=EXCLUDED.uid,resource_version=EXCLUDED.resource_version,labels=EXCLUDED.labels,owner_uids=EXCLUDED.owner_uids,phase=EXCLUDED.phase,readiness=EXCLUDED.readiness,source='kubernetes',observed_at=NOW(),health_summary=EXCLUDED.health_summary`, applicationID, item.Identity.ClusterID, item.Identity.APIVersion, item.Identity.Kind, item.Identity.Namespace, item.Identity.Name, item.UID, item.ResourceVersion, labels, owners, item.Phase, item.Readiness, healthSummary); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertManagedResource(ctx context.Context, applicationID string, resource core.Resource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO managed_resources(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(application_id,cluster_id,api_version,kind,namespace,name) DO UPDATE SET uid=EXCLUDED.uid,resource_version=EXCLUDED.resource_version,manifest=EXCLUDED.manifest,adopted=FALSE,last_seen_at=NOW()`, applicationID, resource.Identity.ClusterID, resource.Identity.APIVersion, resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, resource.UID, resource.ResourceVersion, resource.Manifest)
	return err
}

// AdoptManagedResource records a reviewed takeover without racing another
// takeover. It pauses reconciliation without changing the configured sync policy,
// so the first post-adoption diff requires a fresh user review.
func (s *Store) AdoptManagedResource(ctx context.Context, applicationID, actorID, reason string, resource core.Resource) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	identity := resource.Identity
	lockKey, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, string(lockKey)); err != nil {
		return err
	}
	var active, tracked int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE application_id=$1 AND status IN ('queued','running')`, applicationID).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return errors.New("application is currently syncing")
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_resources WHERE cluster_id=$1 AND api_version=$2 AND kind=$3 AND namespace=$4 AND name=$5`, identity.ClusterID, identity.APIVersion, identity.Kind, identity.Namespace, identity.Name).Scan(&tracked); err != nil {
		return err
	}
	if tracked != 0 {
		return errors.New("resource is already recorded as managed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO managed_resources(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest,adopted) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE)`, applicationID, identity.ClusterID, identity.APIVersion, identity.Kind, identity.Namespace, identity.Name, resource.UID, resource.ResourceVersion, resource.Manifest); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, applicationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,health='unknown',last_checked_at=NULL,updated_at=NOW() WHERE id=$1`, applicationID); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"identity": resource.Identity, "uid": resource.UID, "reason": reason, "autoSyncPaused": true})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'application.resource_adopted','application',$2,$3)`, actorID, applicationID, details); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteManagedResource(ctx context.Context, applicationID string, identity core.Identity) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM managed_resources WHERE application_id=$1 AND cluster_id=$2 AND api_version=$3 AND kind=$4 AND namespace=$5 AND name=$6`, applicationID, identity.ClusterID, identity.APIVersion, identity.Kind, identity.Namespace, identity.Name)
	return err
}

type Operation struct {
	ID                   string            `json:"id"`
	ApplicationID        string            `json:"applicationId"`
	PlanID               *string           `json:"planId,omitempty"`
	ActorID              *string           `json:"actorId,omitempty"`
	ApprovalID           string            `json:"-"`
	ApprovalIDs          []string          `json:"-"`
	Status               string            `json:"status"`
	Type                 string            `json:"type"`
	RollbackCheckpointID string            `json:"rollbackCheckpointId,omitempty"`
	Message              string            `json:"message"`
	Progress             OperationProgress `json:"progress"`
	AttemptCount         int               `json:"attemptCount"`
	ErrorCode            string            `json:"errorCode,omitempty"`
	NextRetryAt          *time.Time        `json:"nextRetryAt,omitempty"`
	TerminalReason       string            `json:"terminalReason,omitempty"`
	StartedAt            time.Time         `json:"startedAt"`
	FinishedAt           *time.Time        `json:"finishedAt,omitempty"`
	TraceParent          string            `json:"-"`
}

type OperationProgress struct {
	Phase     string          `json:"phase,omitempty"`
	Total     int             `json:"total"`
	Completed []core.Identity `json:"completed"`
	Current   *core.Identity  `json:"current,omitempty"`
}

func (s *Store) QueueOperation(ctx context.Context, applicationID, planID, actorID string, approvalIDs []string, planDigest, traceParent string, lease time.Duration, progress OperationProgress) (Operation, error) {
	encoded, err := json.Marshal(progress)
	if err != nil {
		return Operation{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	var lockedApplicationID, priorTerminalReason string
	var priorAttemptCount int
	if err := tx.QueryRowContext(ctx, `SELECT id,retry_attempt_count,retry_terminal_reason FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&lockedApplicationID, &priorAttemptCount, &priorTerminalReason); err != nil {
		return Operation{}, err
	}
	// Recheck under the same lock used by PauseApplication. A poller may
	// have loaded the application before the user paused it.
	if actorID == "justcd-system" {
		var paused, configurationMissing bool
		if err := tx.QueryRowContext(ctx, `SELECT auto_sync_paused,configuration_missing FROM applications WHERE id=$1`, applicationID).Scan(&paused, &configurationMissing); err != nil {
			return Operation{}, err
		}
		if paused || configurationMissing {
			return Operation{}, errors.New("automatic reconciliation is paused or the Git definition is missing")
		}
	}
	attemptCount := priorAttemptCount + 1
	if priorTerminalReason != "" {
		attemptCount = 1
	}
	var status, storedDigest string
	var rollback bool
	var expires time.Time
	if err := tx.QueryRowContext(ctx, `SELECT status,digest,expires_at,rollback_target IS NOT NULL FROM plans WHERE id=$1 AND application_id=$2 FOR UPDATE`, planID, applicationID).Scan(&status, &storedDigest, &expires, &rollback); err != nil {
		return Operation{}, err
	}
	if rollback {
		attemptCount = 1
	}
	if status != "current" || storedDigest != planDigest || !time.Now().Before(expires) {
		return Operation{}, errors.New("plan is no longer current or has expired")
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1)`, applicationID).Scan(&active); err != nil {
		return Operation{}, err
	}
	if active {
		return Operation{}, errors.New("application already has an active operation")
	}
	seen := make(map[string]bool, len(approvalIDs))
	for _, approvalID := range approvalIDs {
		if approvalID == "" || seen[approvalID] {
			return Operation{}, errors.New("approval IDs must be unique and non-empty")
		}
		seen[approvalID] = true
		result, err := tx.ExecContext(ctx, `UPDATE deletion_approvals SET used_at=NOW() WHERE id=$1 AND plan_id=$2 AND plan_digest=$3 AND used_at IS NULL AND expires_at>NOW()`, approvalID, planID, planDigest)
		if err != nil {
			return Operation{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return Operation{}, err
		}
		if count != 1 {
			return Operation{}, errors.New("approval has already been used or expired")
		}
	}
	id := NewID()
	var approval any
	if len(approvalIDs) > 0 {
		approval = approvalIDs[0]
	}
	encodedApprovalIDs, err := json.Marshal(approvalIDs)
	if err != nil {
		return Operation{}, err
	}
	operationType, message := "sync", "Sync queued"
	if rollback {
		operationType, message = "rollback", "Rollback queued"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,actor_id,approval_id,approval_ids,operation_type,status,message,progress,attempt_count,traceparent) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,'queued',$8,$9,$10,$11)`, id, applicationID, planID, actorID, approval, encodedApprovalIDs, operationType, message, encoded, attemptCount, traceParent)
	if err != nil {
		return Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES($1,$2,NOW()+($3 * INTERVAL '1 second'))`, applicationID, id, int64(lease.Seconds())); err != nil {
		return Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',updated_at=NOW() WHERE id=$1`, applicationID); err != nil {
		return Operation{}, err
	}
	auditDetails, err := json.Marshal(map[string]any{"operationId": id, "planId": planID, "digest": planDigest, "approvalIds": approvalIDs})
	if err != nil {
		return Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES(NULLIF($1,''),$2,'application',$3,$4)`, actorID, operationType+".queued", applicationID, auditDetails); err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Operation{}, err
	}
	plan, actor := planID, actorID
	firstApprovalID := ""
	if len(approvalIDs) > 0 {
		firstApprovalID = approvalIDs[0]
	}
	return Operation{ID: id, ApplicationID: applicationID, PlanID: &plan, ActorID: &actor, ApprovalID: firstApprovalID, ApprovalIDs: append([]string(nil), approvalIDs...), Status: "queued", Type: operationType, Progress: progress, AttemptCount: attemptCount, StartedAt: time.Now().UTC(), Message: message, TraceParent: traceParent}, nil
}

// ClaimQueuedOperation atomically claims one durable queue entry. It never
// resumes a running entry: only queued operations may transition to running.
func (s *Store) ClaimQueuedOperation(ctx context.Context, lease time.Duration) (Operation, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, false, err
	}
	defer tx.Rollback()
	var operation Operation
	var progress []byte
	var rawApprovalIDs []byte
	var clusterID string
	var maxConcurrent, operationsPerMinute int
	err = tx.QueryRowContext(ctx, `SELECT o.id,o.application_id,o.plan_id,o.actor_id,COALESCE(o.approval_id,''),o.approval_ids,o.operation_type,COALESCE(o.rollback_checkpoint_id,''),o.status,o.message,o.progress,o.attempt_count,o.error_code,o.next_retry_at,o.terminal_reason,o.started_at,o.finished_at,COALESCE(o.traceparent,''),a.cluster_id,c.max_concurrent_operations,c.operations_per_minute
		FROM operations o
		JOIN applications a ON a.id=o.application_id
		JOIN clusters c ON c.id=a.cluster_id
		JOIN cluster_operation_gates g ON g.cluster_id=c.id
		WHERE o.status='queued' AND (o.next_retry_at IS NULL OR o.next_retry_at<=NOW()) AND g.next_operation_at<=NOW()
		AND (SELECT COUNT(*) FROM operations running
			JOIN applications running_app ON running_app.id=running.application_id
			JOIN operation_leases active_lease ON active_lease.operation_id=running.id
			WHERE running.status='running' AND active_lease.expires_at>NOW() AND running_app.cluster_id=a.cluster_id) < c.max_concurrent_operations
		ORDER BY o.started_at,o.id LIMIT 1 FOR UPDATE OF o,g SKIP LOCKED`).Scan(&operation.ID, &operation.ApplicationID, &operation.PlanID, &operation.ActorID, &operation.ApprovalID, &rawApprovalIDs, &operation.Type, &operation.RollbackCheckpointID, &operation.Status, &operation.Message, &progress, &operation.AttemptCount, &operation.ErrorCode, &operation.NextRetryAt, &operation.TerminalReason, &operation.StartedAt, &operation.FinishedAt, &operation.TraceParent, &clusterID, &maxConcurrent, &operationsPerMinute)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, err
	}
	if err := json.Unmarshal(progress, &operation.Progress); err != nil {
		return Operation{}, false, err
	}
	if err := json.Unmarshal(rawApprovalIDs, &operation.ApprovalIDs); err != nil {
		return Operation{}, false, err
	}
	if len(operation.ApprovalIDs) == 0 && operation.ApprovalID != "" {
		operation.ApprovalIDs = []string{operation.ApprovalID}
	}
	var activeForCluster int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations running JOIN applications running_app ON running_app.id=running.application_id JOIN operation_leases active_lease ON active_lease.operation_id=running.id WHERE running.status='running' AND active_lease.expires_at>NOW() AND running_app.cluster_id=$1`, clusterID).Scan(&activeForCluster); err != nil {
		return Operation{}, false, err
	}
	if activeForCluster >= maxConcurrent {
		return Operation{}, false, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES($1,$2,NOW()+($3 * INTERVAL '1 second')) ON CONFLICT(application_id) DO UPDATE SET operation_id=EXCLUDED.operation_id,expires_at=EXCLUDED.expires_at WHERE operation_leases.expires_at<=NOW() OR operation_leases.operation_id=EXCLUDED.operation_id`, operation.ApplicationID, operation.ID, int64(lease.Seconds()))
	if err != nil {
		return Operation{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Operation{}, false, err
	}
	if count != 1 {
		return Operation{}, false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW()+($2::double precision * INTERVAL '1 second') WHERE cluster_id=$1`, clusterID, 60.0/float64(operationsPerMinute)); err != nil {
		return Operation{}, false, err
	}
	message := "Sync in progress"
	if operation.Type == "rollback" {
		message = "Rollback in progress"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status='running',message=$2 WHERE id=$1 AND status='queued'`, operation.ID, message); err != nil {
		return Operation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Operation{}, false, err
	}
	operation.Status = "running"
	operation.Message = message
	return operation, true, nil
}

// RecoverInterruptedOperations makes expired running jobs visible as failed;
// queued jobs remain eligible for normal processing after a restart.
func (s *Store) RecoverInterruptedOperations(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `WITH interrupted AS (
		UPDATE operations o SET status='failed',message=CASE WHEN o.operation_type='rollback' THEN 'Rollback interrupted by backend restart or worker loss; partial changes remain and the operation will not resume automatically. Review cluster state and any available checkpoint.' ELSE 'Sync interrupted by backend restart or worker loss; review the plan before retrying.' END,error_code='operation.interrupted',next_retry_at=NULL,terminal_reason='interrupted_requires_review',finished_at=NOW()
		WHERE o.status='running' AND NOT EXISTS (SELECT 1 FROM operation_leases l WHERE l.operation_id=o.id AND l.expires_at>NOW())
		RETURNING o.id,o.actor_id,o.application_id,o.plan_id,o.operation_type,o.message
	) INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details)
	SELECT actor_id,operation_type||'.failed','application',application_id,jsonb_build_object('operationId',id,'planId',plan_id,'message',message,'interrupted',true) FROM interrupted`)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE applications a SET auto_sync_paused=TRUE,health='degraded',retry_next_at=NULL,retry_terminal_reason='interrupted_requires_review',retry_last_error_code='operation.interrupted',updated_at=NOW() WHERE a.sync_policy='auto-safe' AND EXISTS (SELECT 1 FROM operations o WHERE o.application_id=a.id AND o.status='failed' AND o.terminal_reason='interrupted_requires_review')`); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM operation_leases l USING operations o WHERE l.operation_id=o.id AND l.expires_at<=NOW() AND o.status<>'queued'`)
	return err
}

func (s *Store) SetOperationProgress(ctx context.Context, id string, progress OperationProgress) error {
	encoded, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE operations SET progress=$2 WHERE id=$1`, id, encoded)
	return err
}

func (s *Store) AcquireOperation(ctx context.Context, applicationID, planID, actorID string, lease time.Duration) (string, error) {
	return s.acquireOperation(ctx, applicationID, planID, actorID, lease, false)
}

func (s *Store) AcquireResourceAction(ctx context.Context, applicationID, actorID string, lease time.Duration) (string, error) {
	return s.acquireOperation(ctx, applicationID, "", actorID, lease, true)
}

func (s *Store) acquireOperation(ctx context.Context, applicationID, planID, actorID string, lease time.Duration, resourceAction bool) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var lockedApplicationID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&lockedApplicationID); err != nil {
		return "", err
	}
	if resourceAction {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, applicationID).Scan(&active); err != nil {
			return "", err
		}
		if active {
			return "", errors.New("application already has an active operation")
		}
		result, err := tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_next_at=NULL,updated_at=NOW() WHERE id=$1 AND NOT decommissioning`, applicationID)
		if err != nil {
			return "", err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		if count != 1 {
			return "", errors.New("application is being decommissioned")
		}
	}
	if planID != "" {
		var status string
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT status,expires_at FROM plans WHERE id=$1 AND application_id=$2 FOR UPDATE`, planID, applicationID).Scan(&status, &expires); err != nil {
			return "", err
		}
		if status != "current" || !time.Now().Before(expires) {
			return "", errors.New("plan is no longer current or has expired")
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE application_id=$1 AND expires_at<=NOW()`, applicationID); err != nil {
		return "", err
	}
	id := NewID()
	var plan any
	if planID != "" {
		plan = planID
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,actor_id,status) VALUES($1,$2,$3,$4,'running')`, id, applicationID, plan, actor); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES($1,$2,NOW()+($3 * INTERVAL '1 second')) ON CONFLICT(application_id) DO NOTHING`, applicationID, id, int64(lease.Seconds()))
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", errors.New("application already has an active operation")
	}
	if resourceAction {
		if _, err := tx.ExecContext(ctx, `UPDATE operations SET operation_type='resource_action' WHERE id=$1`, id); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}
func (s *Store) FinishOperation(ctx context.Context, id, status, message string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET status=$2,message=$3,finished_at=NOW() WHERE id=$1`, id, status, message); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishOperationWithRetry(ctx context.Context, id, applicationID, message string, attempt int, errorCode string, nextRetryAt *time.Time, terminalReason string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status='failed',message=$2,finished_at=NOW(),attempt_count=$3,error_code=$4,next_retry_at=$5,terminal_reason=$6 WHERE id=$1`, id, message, attempt, errorCode, nextRetryAt, terminalReason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET retry_attempt_count=$2,retry_last_error_code=$3,retry_next_at=$4,retry_terminal_reason=$5,health='degraded',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, applicationID, attempt, errorCode, nextRetryAt, terminalReason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) OperationByID(ctx context.Context, id string) (Operation, error) {
	var item Operation
	var rawProgress, rawApprovalIDs []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,application_id,plan_id,actor_id,COALESCE(approval_id,''),approval_ids,operation_type,COALESCE(rollback_checkpoint_id,''),status,message,progress,attempt_count,error_code,next_retry_at,terminal_reason,started_at,finished_at,COALESCE(traceparent,'') FROM operations WHERE id=$1`, id).
		Scan(&item.ID, &item.ApplicationID, &item.PlanID, &item.ActorID, &item.ApprovalID, &rawApprovalIDs, &item.Type, &item.RollbackCheckpointID, &item.Status, &item.Message, &rawProgress, &item.AttemptCount, &item.ErrorCode, &item.NextRetryAt, &item.TerminalReason, &item.StartedAt, &item.FinishedAt, &item.TraceParent)
	if err != nil {
		return Operation{}, err
	}
	if err := json.Unmarshal(rawApprovalIDs, &item.ApprovalIDs); err != nil {
		return Operation{}, err
	}
	if err := json.Unmarshal(rawProgress, &item.Progress); err != nil {
		return Operation{}, err
	}
	if len(item.ApprovalIDs) == 0 && item.ApprovalID != "" {
		item.ApprovalIDs = []string{item.ApprovalID}
	}
	return item, nil
}

func (s *Store) ListOperations(ctx context.Context, applicationID string, limit int) ([]Operation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,application_id,plan_id,actor_id,operation_type,COALESCE(rollback_checkpoint_id,''),status,message,progress,attempt_count,error_code,next_retry_at,terminal_reason,started_at,finished_at,COALESCE(traceparent,'') FROM operations WHERE application_id=$1 ORDER BY started_at DESC LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Operation, 0)
	for rows.Next() {
		var item Operation
		var rawProgress []byte
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.PlanID, &item.ActorID, &item.Type, &item.RollbackCheckpointID, &item.Status, &item.Message, &rawProgress, &item.AttemptCount, &item.ErrorCode, &item.NextRetryAt, &item.TerminalReason, &item.StartedAt, &item.FinishedAt, &item.TraceParent); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawProgress, &item.Progress); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type AuditEvent struct {
	ID           int64           `json:"id"`
	ActorID      *string         `json:"actorId,omitempty"`
	ActorName    string          `json:"actorName"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Details      json.RawMessage `json:"details"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int, before int64) ([]AuditEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.actor_id,COALESCE(NULLIF(u.display_name,''),u.email,''),e.action,e.resource_type,e.resource_id,e.details,e.created_at
		FROM audit_events e LEFT JOIN users u ON u.id=e.actor_id
		WHERE ($2 = 0 OR e.id < $2) ORDER BY e.id DESC LIMIT $1`, limit, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Details = safeAuditDetails(e.Details)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Older rollback events included settings that may contain Helm values.
// Keep target provenance while withholding configuration content from the API.
func safeAuditDetails(raw json.RawMessage) json.RawMessage {
	var details map[string]json.RawMessage
	if json.Unmarshal(raw, &details) != nil {
		return raw
	}
	target, ok := details["rollbackTarget"]
	if !ok || string(target) == "null" {
		return raw
	}
	var rollback map[string]json.RawMessage
	if json.Unmarshal(target, &rollback) != nil {
		return raw
	}
	delete(rollback, "settings")
	safeTarget, err := json.Marshal(rollback)
	if err != nil {
		return raw
	}
	details["rollbackTarget"] = safeTarget
	safeDetails, err := json.Marshal(details)
	if err != nil {
		return raw
	}
	return safeDetails
}
