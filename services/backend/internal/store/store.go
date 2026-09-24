package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/justlab/justcd/services/backend/internal/core"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, connectionString string) (*Store, error) {
	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
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
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	IsAdmin     bool      `json:"isAdmin"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Session struct {
	UserID   string
	CSRFHash []byte
	Expires  time.Time
}

var ErrAlreadyInitialized = errors.New("instance already has a user")

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
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at,password_hash FROM users WHERE LOWER(email)=LOWER($1) AND disabled=FALSE`, email).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt, &passwordHash)
	return user, passwordHash.String, err
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var user User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at FROM users WHERE id=$1 AND disabled=FALSE`, id).
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
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.display_name,u.is_admin,u.created_at FROM oidc_identities i JOIN users u ON u.id=i.user_id WHERE i.provider_id=$1 AND i.subject=$2 AND u.disabled=FALSE`, providerID, subject).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT id,email,display_name,is_admin,created_at FROM users WHERE LOWER(email)=LOWER($1) AND disabled=FALSE`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.CreatedAt)
		if err == sql.ErrNoRows {
			user.ID = NewID()
			user.Email = email
			user.DisplayName = displayName
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
	} else if err != nil {
		return User{}, err
	}
	if displayName != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET display_name=$2 WHERE id=$1 AND display_name=''`, user.ID, displayName); err != nil {
			return User{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM oidc_membership_grants WHERE provider_id=$1 AND user_id=$2`, providerID, user.ID); err != nil {
		return User{}, err
	}
	if len(groups) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO oidc_membership_grants(provider_id,user_id,project_id,role)
			SELECT provider_id,$2,project_id,CASE
			WHEN BOOL_OR(role='owner') THEN 'owner'
			WHEN BOOL_OR(role='deployer') THEN 'deployer'
			ELSE 'viewer' END
			FROM oidc_group_roles WHERE provider_id=$1 AND group_name=ANY($3) GROUP BY provider_id,project_id
			ON CONFLICT(provider_id,user_id,project_id) DO UPDATE SET role=EXCLUDED.role`, providerID, user.ID, groups); err != nil {
			return User{}, err
		}
	}
	return user, tx.Commit()
}

func (s *Store) AddOIDCGroupRole(ctx context.Context, providerID, groupName, projectID, role string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_group_roles(provider_id,group_name,project_id,role) VALUES($1,$2,$3,$4) ON CONFLICT(provider_id,group_name,project_id) DO UPDATE SET role=EXCLUDED.role`, providerID, groupName, projectID, role)
	return err
}

func (s *Store) CreateUser(ctx context.Context, user User, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,is_admin) VALUES($1,LOWER($2),$3,$4,$5)`, user.ID, user.Email, user.DisplayName, passwordHash, user.IsAdmin)
	return err
}
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,email,display_name,is_admin,created_at FROM users WHERE disabled=FALSE ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) SetProjectMember(ctx context.Context, projectID, userID, role string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO project_memberships(project_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT(project_id,user_id) DO UPDATE SET role=EXCLUDED.role`, projectID, userID, role)
	return err
}
func (s *Store) RemoveProjectMember(ctx context.Context, projectID, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	return err
}
func (s *Store) ListProjectMembers(ctx context.Context, projectID string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,u.email,u.display_name,pm.role FROM project_memberships pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 ORDER BY u.email`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, email, name, role string
		if err := rows.Scan(&id, &email, &name, &role); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "email": email, "displayName": name, "role": role})
	}
	return out, rows.Err()
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Role        string    `json:"role,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) ListProjects(ctx context.Context, user User) ([]Project, error) {
	query := `WITH effective AS (
		SELECT project_id,user_id,role FROM project_memberships WHERE user_id=$1
		UNION ALL
		SELECT project_id,user_id,role FROM oidc_membership_grants WHERE user_id=$1
	), ranked AS (
		SELECT project_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank FROM effective GROUP BY project_id
	)
	SELECT p.id,p.name,p.description,CASE COALESCE(ranked.rank,3) WHEN 3 THEN 'owner' WHEN 2 THEN 'deployer' ELSE 'viewer' END,p.created_at
	FROM projects p LEFT JOIN ranked ON ranked.project_id=p.id WHERE $2 OR ranked.project_id IS NOT NULL ORDER BY p.name`
	rows, err := s.DB.QueryContext(ctx, query, user.ID, user.IsAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.Role, &project.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) CreateProject(ctx context.Context, project Project, ownerID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,description) VALUES($1,$2,$3)`, project.ID, project.Name, project.Description); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_memberships(project_id,user_id,role) VALUES($1,$2,'owner')`, project.ID, ownerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateProject(ctx context.Context, id, name, description string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE projects SET name=$2,description=$3,updated_at=NOW() WHERE id=$1`, id, name, description)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteProjectKeepingResources(ctx context.Context, id string, requireEmpty bool) (int, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID); err != nil {
		return 0, 0, err
	}
	var apps, managed, active int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM applications WHERE project_id=$1), (SELECT COUNT(*) FROM managed_resources m JOIN applications a ON a.id=m.application_id WHERE a.project_id=$1), (SELECT COUNT(*) FROM operations o JOIN applications a ON a.id=o.application_id WHERE a.project_id=$1 AND o.status IN ('queued','running'))`, id).Scan(&apps, &managed, &active); err != nil {
		return 0, 0, err
	}
	if active > 0 {
		return 0, 0, errors.New("project has active sync operations")
	}
	if requireEmpty && managed > 0 {
		return 0, 0, errors.New("project still manages Kubernetes resources")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=$1`, id); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return apps, managed, nil
}

func (s *Store) ProjectRole(ctx context.Context, user User, projectID string) (string, error) {
	if user.IsAdmin {
		return "owner", nil
	}
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT role FROM (
		SELECT role FROM project_memberships WHERE project_id=$1 AND user_id=$2
		UNION ALL SELECT role FROM oidc_membership_grants WHERE project_id=$1 AND user_id=$2
	) roles ORDER BY CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END DESC LIMIT 1`, projectID, user.ID).Scan(&role)
	return role, err
}

func (s *Store) ProjectRoleForUser(ctx context.Context, projectID, userID string) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT role FROM (
		SELECT role FROM project_memberships WHERE project_id=$1 AND user_id=$2
		UNION ALL SELECT role FROM oidc_membership_grants WHERE project_id=$1 AND user_id=$2
	) roles ORDER BY CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END DESC LIMIT 1`, projectID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func (s *Store) ProjectOwnerCount(ctx context.Context, projectID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT user_id,MAX(CASE role WHEN 'owner' THEN 3 WHEN 'deployer' THEN 2 ELSE 1 END) AS rank
		FROM (
			SELECT user_id,role FROM project_memberships WHERE project_id=$1
			UNION ALL SELECT user_id,role FROM oidc_membership_grants WHERE project_id=$1
		) members GROUP BY user_id
	) effective WHERE rank=3`, projectID).Scan(&count)
	return count, err
}

type Credential struct {
	ID        string     `json:"id"`
	ProjectID *string    `json:"projectId,omitempty"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Username  string     `json:"username,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Cipher    []byte     `json:"-"`
}

func (s *Store) CreateCredential(ctx context.Context, c Credential) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO credentials(id,project_id,name,kind,secret_cipher,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, c.ID, c.ProjectID, c.Name, c.Kind, c.Cipher, c.ExpiresAt)
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
	err := s.DB.QueryRowContext(ctx, `SELECT id,project_id,name,kind,secret_cipher,expires_at,created_at FROM credentials WHERE id=$1`, id).Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &c.Cipher, &c.ExpiresAt, &c.CreatedAt)
	return c, err
}

func (s *Store) ListCredentials(ctx context.Context, projectID string) ([]Credential, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,project_id,name,kind,secret_cipher,expires_at,created_at FROM credentials WHERE project_id=$1 OR project_id IS NULL ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Credential, 0)
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &c.Cipher, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

type Cluster struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	APIServer              string    `json:"apiServer"`
	CAData                 []byte    `json:"-"`
	InsecureSkipVerify     bool      `json:"insecureSkipVerify"`
	DefaultCredentialID    *string   `json:"defaultCredentialId,omitempty"`
	ClusterScopeCredential *string   `json:"clusterScopeCredentialId,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
}

func (s *Store) CreateCluster(ctx context.Context, c Cluster) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, c.ID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential)
	return err
}

func (s *Store) CreateClusterWithProjectCredential(ctx context.Context, c Cluster, projectID, credentialID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, c.ID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_cluster_credentials(project_id,cluster_id,credential_id) VALUES($1,$2,$3)`, projectID, c.ID, credentialID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ProjectClusterCredential(ctx context.Context, projectID, clusterID string) (*string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT credential_id FROM project_cluster_credentials WHERE project_id=$1 AND cluster_id=$2`, projectID, clusterID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Store) SetProjectClusterCredential(ctx context.Context, projectID, clusterID string, credentialID *string) error {
	if credentialID == nil {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM project_cluster_credentials WHERE project_id=$1 AND cluster_id=$2`, projectID, clusterID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO project_cluster_credentials(project_id,cluster_id,credential_id) VALUES($1,$2,$3) ON CONFLICT (project_id,cluster_id) DO UPDATE SET credential_id=EXCLUDED.credential_id,updated_at=NOW()`, projectID, clusterID, *credentialID)
	return err
}
func (s *Store) ClusterByID(ctx context.Context, id string) (Cluster, error) {
	var c Cluster
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,api_server,ca_data,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,created_at FROM clusters WHERE id=$1`, id).Scan(&c.ID, &c.Name, &c.APIServer, &c.CAData, &c.InsecureSkipVerify, &c.DefaultCredentialID, &c.ClusterScopeCredential, &c.CreatedAt)
	return c, err
}
func (s *Store) ListClusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,api_server,insecure_skip_verify,default_credential_id,cluster_scope_credential_id,created_at FROM clusters ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Cluster, 0)
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.APIServer, &c.InsecureSkipVerify, &c.DefaultCredentialID, &c.ClusterScopeCredential, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCluster(ctx context.Context, c Cluster) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE clusters SET name=$2,api_server=$3,ca_data=$4,insecure_skip_verify=$5,default_credential_id=$6,cluster_scope_credential_id=$7,updated_at=NOW() WHERE id=$1`, c.ID, c.Name, c.APIServer, c.CAData, c.InsecureSkipVerify, c.DefaultCredentialID, c.ClusterScopeCredential)
}

func (s *Store) CreateNamespaceBinding(ctx context.Context, projectID, clusterID, namespace string, credentialID *string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO namespace_bindings(id,project_id,cluster_id,namespace,credential_id) VALUES($1,$2,$3,$4,$5)`, NewID(), projectID, clusterID, namespace, credentialID)
	return err
}

func (s *Store) UpdateNamespaceBinding(ctx context.Context, projectID, clusterID, namespace string, credentialID *string) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE namespace_bindings SET credential_id=$4 WHERE project_id=$1 AND cluster_id=$2 AND namespace=$3`, projectID, clusterID, namespace, credentialID)
}

func (s *Store) NamespaceBinding(ctx context.Context, projectID, clusterID, namespace string) (NamespaceBinding, error) {
	var binding NamespaceBinding
	err := s.DB.QueryRowContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE project_id=$1 AND cluster_id=$2 AND namespace=$3`, projectID, clusterID, namespace).Scan(&binding.Namespace, &binding.CredentialID)
	return binding, err
}

func (s *Store) ListNamespaceBindings(ctx context.Context, projectID, clusterID string) ([]NamespaceBinding, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE project_id=$1 AND cluster_id=$2 ORDER BY namespace`, projectID, clusterID)
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

type GitSource struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	Name          string    `json:"name"`
	RepositoryURL string    `json:"repositoryUrl"`
	CredentialID  *string   `json:"credentialId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (s *Store) CreateGitSource(ctx context.Context, v GitSource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO git_sources(id,project_id,name,repository_url,credential_id) VALUES($1,$2,$3,$4,$5)`, v.ID, v.ProjectID, v.Name, v.RepositoryURL, v.CredentialID)
	return err
}
func (s *Store) UpdateGitSource(ctx context.Context, v GitSource) error {
	return s.updateConnectionAndInvalidate(ctx, `UPDATE git_sources SET name=$2,repository_url=$3,credential_id=$4,updated_at=NOW() WHERE id=$1`, v.ID, v.Name, v.RepositoryURL, v.CredentialID)
}
func (s *Store) GitSourceByID(ctx context.Context, id string) (GitSource, error) {
	var v GitSource
	err := s.DB.QueryRowContext(ctx, `SELECT id,project_id,name,repository_url,credential_id,created_at FROM git_sources WHERE id=$1`, id).Scan(&v.ID, &v.ProjectID, &v.Name, &v.RepositoryURL, &v.CredentialID, &v.CreatedAt)
	return v, err
}
func (s *Store) ListGitSources(ctx context.Context, projectID string) ([]GitSource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,project_id,name,repository_url,credential_id,created_at FROM git_sources WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GitSource, 0)
	for rows.Next() {
		var v GitSource
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.RepositoryURL, &v.CredentialID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Application struct {
	ID                         string             `json:"id"`
	ProjectID                  string             `json:"projectId"`
	Name                       string             `json:"name"`
	SourceID                   string             `json:"sourceId"`
	Revision                   string             `json:"revision"`
	ManifestPath               string             `json:"manifestPath"`
	Renderer                   string             `json:"renderer"`
	KustomizeHelmEnabled       bool               `json:"kustomizeHelmEnabled"`
	KustomizeNamespaceOverride bool               `json:"kustomizeNamespaceOverride"`
	ClusterID                  string             `json:"clusterId"`
	Namespaces                 []NamespaceBinding `json:"namespaces"`
	SyncPolicy                 string             `json:"syncPolicy"`
	PollSeconds                int                `json:"pollSeconds"`
	LastCheckedAt              *time.Time         `json:"lastCheckedAt,omitempty"`
	LastSyncedRevision         string             `json:"lastSyncedRevision,omitempty"`
	Health                     string             `json:"health"`
	Decommissioning            bool               `json:"decommissioning"`
	CreatedAt                  time.Time          `json:"createdAt"`
}

func (s *Store) CreateApplication(ctx context.Context, a Application) error {
	namespaces, err := json.Marshal(a.Namespaces)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO applications(id,project_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,cluster_id,namespaces,sync_policy,poll_seconds) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, a.ID, a.ProjectID, a.Name, a.SourceID, a.Revision, a.ManifestPath, a.Renderer, a.KustomizeHelmEnabled, a.KustomizeNamespaceOverride, a.ClusterID, namespaces, a.SyncPolicy, a.PollSeconds)
	return err
}

func scanApplication(row interface{ Scan(...any) error }) (Application, error) {
	var a Application
	var namespaces []byte
	err := row.Scan(&a.ID, &a.ProjectID, &a.Name, &a.SourceID, &a.Revision, &a.ManifestPath, &a.Renderer, &a.KustomizeHelmEnabled, &a.KustomizeNamespaceOverride, &a.ClusterID, &namespaces, &a.SyncPolicy, &a.PollSeconds, &a.LastCheckedAt, &a.LastSyncedRevision, &a.Health, &a.Decommissioning, &a.CreatedAt)
	if err == nil {
		err = json.Unmarshal(namespaces, &a.Namespaces)
	}
	return a, err
}

const applicationColumns = `id,project_id,name,source_id,revision,manifest_path,renderer,kustomize_helm_enabled,kustomize_namespace_override,cluster_id,namespaces,sync_policy,poll_seconds,last_checked_at,COALESCE(last_synced_revision,''),health,decommissioning,created_at`

func (s *Store) ApplicationByID(ctx context.Context, id string) (Application, error) {
	return scanApplication(s.DB.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1`, id))
}

func (s *Store) UpdateApplication(ctx context.Context, app Application) error {
	namespaces, err := json.Marshal(app.Namespaces)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldCluster string
	var oldNamespaces []byte
	if err := tx.QueryRowContext(ctx, `SELECT cluster_id,namespaces FROM applications WHERE id=$1 FOR UPDATE`, app.ID).Scan(&oldCluster, &oldNamespaces); err != nil {
		return err
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
	if managed > 0 && (oldCluster != app.ClusterID || string(oldNamespaces) != string(namespaces)) {
		return errors.New("cannot change cluster or namespace bindings while resources are managed")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET name=$2,source_id=$3,revision=$4,manifest_path=$5,renderer=$6,kustomize_helm_enabled=$7,kustomize_namespace_override=$8,cluster_id=$9,namespaces=$10,sync_policy=$11,poll_seconds=$12,last_checked_at=NULL,health='unknown',updated_at=NOW() WHERE id=$1`, app.ID, app.Name, app.SourceID, app.Revision, app.ManifestPath, app.Renderer, app.KustomizeHelmEnabled, app.KustomizeNamespaceOverride, app.ClusterID, namespaces, app.SyncPolicy, app.PollSeconds); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE application_id=$1 AND status='current'`, app.ID); err != nil {
		return err
	}
	return tx.Commit()
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
func (s *Store) ListApplications(ctx context.Context, projectID string) ([]Application, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE project_id=$1 ORDER BY name`, projectID)
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

func (s *Store) DueApplications(ctx context.Context, limit int) ([]Application, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications a WHERE NOT a.decommissioning AND (a.last_checked_at IS NULL OR a.last_checked_at<=NOW()-(a.poll_seconds * INTERVAL '1 second')) AND NOT EXISTS (SELECT 1 FROM operation_leases l WHERE l.application_id=a.id AND l.expires_at>NOW()) ORDER BY a.last_checked_at ASC NULLS FIRST LIMIT $1`, limit)
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
func (s *Store) MarkApplicationSynced(ctx context.Context, id, revision, health string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE applications SET last_synced_revision=$2,health=$3,last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, id, revision, health)
	return err
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
	Desired   []core.Resource `json:"desired"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	Status    string          `json:"status"`
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
	_, err = tx.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,bindings,changes,desired,created_by,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, record.ID, record.Plan.ApplicationID, record.Plan.Revision, record.Plan.Digest, bindings, changes, desired, record.CreatedBy, record.ExpiresAt, record.Status, ignored, selection, record.Plan.IgnoreRulesDigest, record.Plan.Decommission)
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
	var bindings, changes, desired, ignored, selection []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,application_id,revision,digest,bindings,changes,desired,created_by,created_at,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission FROM plans WHERE id=$1`, id).Scan(&out.ID, &out.Plan.ApplicationID, &out.Plan.Revision, &out.Plan.Digest, &bindings, &changes, &desired, &out.CreatedBy, &out.CreatedAt, &out.ExpiresAt, &out.Status, &ignored, &selection, &out.Plan.IgnoreRulesDigest, &out.Plan.Decommission)
	if err != nil {
		return PlanRecord{}, err
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id,application_id,revision,digest,bindings,changes,desired,created_by,created_at,expires_at,status,ignored_changes,selection,ignore_rules_digest,decommission FROM plans WHERE application_id=$1 ORDER BY created_at DESC LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlanRecord, 0)
	for rows.Next() {
		var out PlanRecord
		var bindings, changes, desired, ignored, selection []byte
		if err := rows.Scan(&out.ID, &out.Plan.ApplicationID, &out.Plan.Revision, &out.Plan.Digest, &bindings, &changes, &desired, &out.CreatedBy, &out.CreatedAt, &out.ExpiresAt, &out.Status, &ignored, &selection, &out.Plan.IgnoreRulesDigest, &out.Plan.Decommission); err != nil {
			return nil, err
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
		if err := json.Unmarshal(desired, &out.Desired); err != nil {
			return nil, err
		}
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id,cluster_id,api_version,kind,namespace,name,path,reason,application_id,created_by,created_at FROM application_ignore_rules WHERE application_id=$1 ORDER BY created_at,id`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ApplicationIgnoreRule, 0)
	for rows.Next() {
		var item ApplicationIgnoreRule
		if err := rows.Scan(&item.ID, &item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.Path, &item.Reason, &item.ApplicationID, &item.CreatedBy, &item.CreatedAt); err != nil {
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
	err = tx.QueryRowContext(ctx, `DELETE FROM application_ignore_rules WHERE application_id=$1 AND id=$2 RETURNING id,cluster_id,api_version,kind,namespace,name,path,reason,application_id,created_by,created_at`, applicationID, id).Scan(&item.ID, &item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.Path, &item.Reason, &item.ApplicationID, &item.CreatedBy, &item.CreatedAt)
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id,api_version,kind,label_key,label_value,reason,created_by,created_at FROM application_ignore_selectors WHERE application_id=$1 ORDER BY created_at,id`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]core.IgnoreSelector, 0)
	for rows.Next() {
		var item core.IgnoreSelector
		if err := rows.Scan(&item.ID, &item.APIVersion, &item.Kind, &item.LabelKey, &item.LabelValue, &item.Reason, &item.CreatedBy, &item.CreatedAt); err != nil {
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

func (s *Store) CreateApproval(ctx context.Context, id, planID string, approval core.DeletionApproval, expires time.Time) error {
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
	_, err = s.DB.ExecContext(ctx, `INSERT INTO deletion_approvals(id,plan_id,actor_id,plan_digest,deletes,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, id, planID, approval.ActorID, approval.PlanDigest, payload, expires)
	return err
}

type ApprovalRecord struct {
	ID       string
	PlanID   string
	Approval core.DeletionApproval
	UsedAt   *time.Time
}

func (s *Store) ApprovalByID(ctx context.Context, id string) (ApprovalRecord, error) {
	var out ApprovalRecord
	var payload []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,plan_id,actor_id,plan_digest,deletes,expires_at,used_at FROM deletion_approvals WHERE id=$1`, id).Scan(&out.ID, &out.PlanID, &out.Approval.ActorID, &out.Approval.PlanDigest, &payload, &out.Approval.ExpiresAt, &out.UsedAt)
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
}

func (s *Store) ManagedResources(ctx context.Context, applicationID string) ([]ManagedResource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest FROM managed_resources WHERE application_id=$1 ORDER BY api_version,kind,namespace,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ManagedResource, 0)
	for rows.Next() {
		var item ManagedResource
		if err := rows.Scan(&item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.UID, &item.ResourceVersion, &item.Manifest); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type ObservedResource struct {
	Identity        core.Identity     `json:"identity"`
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels"`
	OwnerUIDs       []string          `json:"ownerUids"`
	Phase           string            `json:"phase,omitempty"`
	Readiness       string            `json:"readiness,omitempty"`
	Source          string            `json:"source"`
	ObservedAt      time.Time         `json:"observedAt"`
}

func (s *Store) ObservedResources(ctx context.Context, applicationID string) ([]ObservedResource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cluster_id,api_version,kind,namespace,name,uid,resource_version,labels,owner_uids,phase,readiness,source,observed_at FROM application_resource_observations WHERE application_id=$1 ORDER BY kind,namespace,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ObservedResource{}
	for rows.Next() {
		var item ObservedResource
		var labels, owners []byte
		if err := rows.Scan(&item.Identity.ClusterID, &item.Identity.APIVersion, &item.Identity.Kind, &item.Identity.Namespace, &item.Identity.Name, &item.UID, &item.ResourceVersion, &labels, &owners, &item.Phase, &item.Readiness, &item.Source, &item.ObservedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(labels, &item.Labels); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(owners, &item.OwnerUIDs); err != nil {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_resource_observations(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,labels,owner_uids,phase,readiness,source,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'kubernetes',NOW()) ON CONFLICT(application_id,cluster_id,api_version,kind,namespace,name) DO UPDATE SET uid=EXCLUDED.uid,resource_version=EXCLUDED.resource_version,labels=EXCLUDED.labels,owner_uids=EXCLUDED.owner_uids,phase=EXCLUDED.phase,readiness=EXCLUDED.readiness,source='kubernetes',observed_at=NOW()`, applicationID, item.Identity.ClusterID, item.Identity.APIVersion, item.Identity.Kind, item.Identity.Namespace, item.Identity.Name, item.UID, item.ResourceVersion, labels, owners, item.Phase, item.Readiness); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertManagedResource(ctx context.Context, applicationID string, resource core.Resource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO managed_resources(application_id,cluster_id,api_version,kind,namespace,name,uid,resource_version,manifest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(application_id,cluster_id,api_version,kind,namespace,name) DO UPDATE SET uid=EXCLUDED.uid,resource_version=EXCLUDED.resource_version,manifest=EXCLUDED.manifest,last_seen_at=NOW()`, applicationID, resource.Identity.ClusterID, resource.Identity.APIVersion, resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, resource.UID, resource.ResourceVersion, resource.Manifest)
	return err
}
func (s *Store) DeleteManagedResource(ctx context.Context, applicationID string, identity core.Identity) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM managed_resources WHERE application_id=$1 AND cluster_id=$2 AND api_version=$3 AND kind=$4 AND namespace=$5 AND name=$6`, applicationID, identity.ClusterID, identity.APIVersion, identity.Kind, identity.Namespace, identity.Name)
	return err
}

type Operation struct {
	ID            string            `json:"id"`
	ApplicationID string            `json:"applicationId"`
	PlanID        *string           `json:"planId,omitempty"`
	ActorID       *string           `json:"actorId,omitempty"`
	ApprovalID    string            `json:"-"`
	Status        string            `json:"status"`
	Message       string            `json:"message"`
	Progress      OperationProgress `json:"progress"`
	StartedAt     time.Time         `json:"startedAt"`
	FinishedAt    *time.Time        `json:"finishedAt,omitempty"`
}

type OperationProgress struct {
	Phase     string          `json:"phase,omitempty"`
	Total     int             `json:"total"`
	Completed []core.Identity `json:"completed"`
	Current   *core.Identity  `json:"current,omitempty"`
}

func (s *Store) QueueOperation(ctx context.Context, applicationID, planID, actorID, approvalID, planDigest string, lease time.Duration, progress OperationProgress) (Operation, error) {
	encoded, err := json.Marshal(progress)
	if err != nil {
		return Operation{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	var lockedApplicationID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&lockedApplicationID); err != nil {
		return Operation{}, err
	}
	var status string
	var expires time.Time
	if err := tx.QueryRowContext(ctx, `SELECT status,expires_at FROM plans WHERE id=$1 AND application_id=$2 FOR UPDATE`, planID, applicationID).Scan(&status, &expires); err != nil {
		return Operation{}, err
	}
	if status != "current" || !time.Now().Before(expires) {
		return Operation{}, errors.New("plan is no longer current or has expired")
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE application_id=$1)`, applicationID).Scan(&active); err != nil {
		return Operation{}, err
	}
	if active {
		return Operation{}, errors.New("application already has an active operation")
	}
	if approvalID != "" {
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
	if approvalID != "" {
		approval = approvalID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,actor_id,approval_id,status,message,progress) VALUES($1,$2,$3,NULLIF($4,''),$5,'queued','Sync queued',$6)`, id, applicationID, planID, actorID, approval, encoded)
	if err != nil {
		return Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES($1,$2,NOW()+($3 * INTERVAL '1 second'))`, applicationID, id, int64(lease.Seconds())); err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Operation{}, err
	}
	plan, actor := planID, actorID
	return Operation{ID: id, ApplicationID: applicationID, PlanID: &plan, ActorID: &actor, ApprovalID: approvalID, Status: "queued", Progress: progress, StartedAt: time.Now().UTC(), Message: "Sync queued"}, nil
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
	err = tx.QueryRowContext(ctx, `SELECT id,application_id,plan_id,actor_id,approval_id,status,message,progress,started_at,finished_at FROM operations WHERE status='queued' ORDER BY started_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&operation.ID, &operation.ApplicationID, &operation.PlanID, &operation.ActorID, &operation.ApprovalID, &operation.Status, &operation.Message, &progress, &operation.StartedAt, &operation.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, err
	}
	if err := json.Unmarshal(progress, &operation.Progress); err != nil {
		return Operation{}, false, err
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
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status='running',message='Sync in progress' WHERE id=$1 AND status='queued'`, operation.ID); err != nil {
		return Operation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Operation{}, false, err
	}
	operation.Status = "running"
	operation.Message = "Sync in progress"
	return operation, true, nil
}

// RecoverInterruptedOperations makes expired running jobs visible as failed;
// queued jobs remain eligible for normal processing after a restart.
func (s *Store) RecoverInterruptedOperations(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE operations o SET status='failed',message='Sync interrupted by backend restart or worker loss; review the plan before retrying',finished_at=NOW() WHERE o.status='running' AND NOT EXISTS (SELECT 1 FROM operation_leases l WHERE l.operation_id=o.id AND l.expires_at>NOW())`)
	if err != nil {
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
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var lockedApplicationID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&lockedApplicationID); err != nil {
		return "", err
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
func (s *Store) ListOperations(ctx context.Context, applicationID string, limit int) ([]Operation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,application_id,plan_id,actor_id,status,message,progress,started_at,finished_at FROM operations WHERE application_id=$1 ORDER BY started_at DESC LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Operation, 0)
	for rows.Next() {
		var item Operation
		var rawProgress []byte
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.PlanID, &item.ActorID, &item.Status, &item.Message, &rawProgress, &item.StartedAt, &item.FinishedAt); err != nil {
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
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Details      json.RawMessage `json:"details"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,actor_id,action,resource_type,resource_id,details,created_at FROM audit_events ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
