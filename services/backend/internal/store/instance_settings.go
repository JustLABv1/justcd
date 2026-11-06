package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// AccessSettings are the instance-wide sign-in and registration controls.
type AccessSettings struct {
	// LocalLoginEnabled allows email and password sign-in. Single sign-on is unaffected.
	LocalLoginEnabled bool `json:"localLoginEnabled"`
	// LocalRegistrationEnabled lets anyone create a password account. Such accounts stay unverified, with no
	// workspace access, until an administrator approves them. It requires LocalLoginEnabled.
	LocalRegistrationEnabled bool `json:"localRegistrationEnabled"`
	// OIDCAutoProvision lets a verified single sign-on identity create its own account on first sign-in.
	// When off, only people an administrator already added can sign in.
	OIDCAutoProvision bool `json:"oidcAutoProvision"`
	// OIDCAllowedDomains limits which email domains may create an account on first sign-in; empty allows any.
	OIDCAllowedDomains []string `json:"oidcAllowedDomains"`
	// WorkspaceCreation is "anyone" (every signed-in user) or "admins".
	WorkspaceCreation string `json:"workspaceCreation"`
}

type InstanceSettings struct {
	MaintenanceEnabled bool `json:"maintenanceEnabled"`
	AccessSettings
}

// AccessError is a rejected settings change whose message is safe to show to the administrator.
type AccessError struct{ Message string }

func (e *AccessError) Error() string { return e.Message }

var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

const maxAllowedDomains = 50

// NormalizeDomains lower-cases, trims, de-duplicates and validates email domains.
func NormalizeDomains(domains []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(domains))
	for _, raw := range domains {
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
		if domain == "" || seen[domain] {
			continue
		}
		if len(domain) > 253 || !domainPattern.MatchString(domain) {
			return nil, &AccessError{fmt.Sprintf("%q is not a valid email domain", raw)}
		}
		seen[domain] = true
		out = append(out, domain)
	}
	if len(out) > maxAllowedDomains {
		return nil, &AccessError{fmt.Sprintf("at most %d domains are allowed", maxAllowedDomains)}
	}
	return out, nil
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func splitDomains(joined string) []string {
	if joined == "" {
		return []string{}
	}
	return strings.Split(joined, ",")
}

func (s *Store) InstanceSettings(ctx context.Context) (InstanceSettings, error) {
	var settings InstanceSettings
	var domains string
	err := s.DB.QueryRowContext(ctx, `SELECT maintenance_enabled,local_login_enabled,local_registration_enabled,oidc_auto_provision,array_to_string(oidc_allowed_domains,','),workspace_creation FROM instance_settings WHERE singleton=TRUE`).
		Scan(&settings.MaintenanceEnabled, &settings.LocalLoginEnabled, &settings.LocalRegistrationEnabled, &settings.OIDCAutoProvision, &domains, &settings.WorkspaceCreation)
	settings.OIDCAllowedDomains = splitDomains(domains)
	return settings, err
}

// AccessReadiness says whether password sign-in can be switched off without locking everyone out.
type AccessReadiness struct {
	EnabledProviders int `json:"enabledProviders"`
	// SSOAdmins counts active administrators who have signed in through an enabled provider.
	SSOAdmins int `json:"ssoAdmins"`
}

func (r AccessReadiness) CanDisableLocalLogin() *AccessError {
	switch {
	case r.EnabledProviders == 0:
		return &AccessError{"Enable at least one single sign-on provider before turning off password sign-in."}
	case r.SSOAdmins == 0:
		return &AccessError{"No administrator has signed in with single sign-on yet. Sign in once with an administrator account through a provider, then turn off password sign-in."}
	}
	return nil
}

func (s *Store) AccessReadiness(ctx context.Context) (AccessReadiness, error) {
	var readiness AccessReadiness
	err := s.DB.QueryRowContext(ctx, accessReadinessQuery).Scan(&readiness.EnabledProviders, &readiness.SSOAdmins)
	return readiness, err
}

const accessReadinessQuery = `SELECT
 (SELECT COUNT(*) FROM oidc_providers WHERE enabled),
 (SELECT COUNT(DISTINCT u.id) FROM users u JOIN oidc_identities i ON i.user_id=u.id JOIN oidc_providers p ON p.id=i.provider_id AND p.enabled
  WHERE u.is_admin AND NOT u.disabled AND u.deleted_at IS NULL AND u.id<>'justcd-system')`

// UpdateAccessSettings applies a validated change and records what changed. Turning off password
// sign-in is refused unless someone can still sign in through single sign-on.
func (s *Store) UpdateAccessSettings(ctx context.Context, next AccessSettings, actorID string) (AccessSettings, error) {
	domains, err := NormalizeDomains(next.OIDCAllowedDomains)
	if err != nil {
		return AccessSettings{}, err
	}
	next.OIDCAllowedDomains = domains
	if next.WorkspaceCreation != "anyone" && next.WorkspaceCreation != "admins" {
		return AccessSettings{}, &AccessError{"workspaceCreation must be anyone or admins"}
	}
	// Accounts that cannot sign in are useless, so registration follows password sign-in.
	if next.LocalRegistrationEnabled && !next.LocalLoginEnabled {
		return AccessSettings{}, &AccessError{"Turn on password sign-in before allowing people to register with a password."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return AccessSettings{}, err
	}
	defer tx.Rollback()
	var previous AccessSettings
	var joined string
	if err := tx.QueryRowContext(ctx, `SELECT local_login_enabled,local_registration_enabled,oidc_auto_provision,array_to_string(oidc_allowed_domains,','),workspace_creation FROM instance_settings WHERE singleton=TRUE FOR UPDATE`).
		Scan(&previous.LocalLoginEnabled, &previous.LocalRegistrationEnabled, &previous.OIDCAutoProvision, &joined, &previous.WorkspaceCreation); err != nil {
		return AccessSettings{}, err
	}
	previous.OIDCAllowedDomains = splitDomains(joined)
	if previous.LocalLoginEnabled && !next.LocalLoginEnabled {
		var readiness AccessReadiness
		if err := tx.QueryRowContext(ctx, accessReadinessQuery).Scan(&readiness.EnabledProviders, &readiness.SSOAdmins); err != nil {
			return AccessSettings{}, err
		}
		if blocked := readiness.CanDisableLocalLogin(); blocked != nil {
			return AccessSettings{}, blocked
		}
	}
	changes := map[string]any{}
	if previous.LocalLoginEnabled != next.LocalLoginEnabled {
		changes["localLoginEnabled"] = next.LocalLoginEnabled
	}
	if previous.LocalRegistrationEnabled != next.LocalRegistrationEnabled {
		changes["localRegistrationEnabled"] = next.LocalRegistrationEnabled
	}
	if previous.OIDCAutoProvision != next.OIDCAutoProvision {
		changes["oidcAutoProvision"] = next.OIDCAutoProvision
	}
	if strings.Join(previous.OIDCAllowedDomains, ",") != strings.Join(next.OIDCAllowedDomains, ",") {
		changes["oidcAllowedDomains"] = next.OIDCAllowedDomains
	}
	if previous.WorkspaceCreation != next.WorkspaceCreation {
		changes["workspaceCreation"] = next.WorkspaceCreation
	}
	if len(changes) == 0 {
		return previous, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE instance_settings SET local_login_enabled=$1,local_registration_enabled=$2,oidc_auto_provision=$3,oidc_allowed_domains=string_to_array($4,','),workspace_creation=$5 WHERE singleton=TRUE`,
		next.LocalLoginEnabled, next.LocalRegistrationEnabled, next.OIDCAutoProvision, strings.Join(next.OIDCAllowedDomains, ","), next.WorkspaceCreation); err != nil {
		return AccessSettings{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'instance.access_updated','instance','instance',$2::jsonb)`, actorID, mustJSON(changes)); err != nil {
		return AccessSettings{}, err
	}
	return next, tx.Commit()
}

func (s *Store) SetMaintenance(ctx context.Context, enabled bool, actorID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous bool
	if err := tx.QueryRowContext(ctx, `SELECT maintenance_enabled FROM instance_settings WHERE singleton=TRUE FOR UPDATE`).Scan(&previous); err != nil {
		return err
	}
	if previous == enabled {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE instance_settings SET maintenance_enabled=$1 WHERE singleton=TRUE`, enabled); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'instance.maintenance_updated','instance','instance',jsonb_build_object('maintenanceEnabled',$2::boolean))`, actorID, enabled); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrEmailTaken means an account with that email address already exists.
var ErrEmailTaken = errors.New("an account with this email address already exists")

// CreateSelfRegisteredUser adds a non-administrator password account whose email address is not verified.
func (s *Store) CreateSelfRegisteredUser(ctx context.Context, user User, passwordHash string) (User, error) {
	user.IsAdmin, user.EmailVerified = false, false
	err := s.DB.QueryRowContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,is_admin,email_verified) VALUES($1,LOWER($2),$3,$4,FALSE,FALSE) RETURNING created_at`,
		user.ID, user.Email, user.DisplayName, passwordHash).Scan(&user.CreatedAt)
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return User{}, ErrEmailTaken
	}
	return user, err
}

// ApproveUser records that an administrator vouches for a self-registered account.
func (s *Store) ApproveUser(ctx context.Context, userID, actorID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET email_verified=TRUE WHERE id=$1 AND email_verified=FALSE AND deleted_at IS NULL`, userID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'user.approved','user',$2,'{}'::jsonb)`, actorID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// UserEmailVerified reports whether an account may be given workspace access.
func (s *Store) UserEmailVerified(ctx context.Context, userID string) (bool, error) {
	var verified bool
	err := s.DB.QueryRowContext(ctx, `SELECT email_verified FROM users WHERE id=$1 AND disabled=FALSE AND deleted_at IS NULL`, userID).Scan(&verified)
	return verified, err
}
