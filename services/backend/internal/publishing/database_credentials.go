package publishing

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DatabaseCredentials never belongs in Config, generated files or API responses.
type DatabaseCredentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	SQL         string `json:"sql"`
	DatabaseSQL string `json:"databaseSql"`
}

func ManagedDatabase(c Config) (*DatabasePlan, error) {
	r, e := resolvedServices(c)
	if e != nil {
		return nil, e
	}
	if r.Database == nil || r.Database.CredentialMode != "generated" {
		return nil, nil
	}
	return r.Database, nil
}
func SetDatabaseCredentialID(c *Config, id string) error {
	r, e := resolvedServices(*c)
	if e != nil {
		return e
	}
	if r.Database != nil && r.Database.CredentialMode == "generated" {
		r.Database.CredentialID = id
		b, _ := json.Marshal(r)
		c.Services = string(b)
	}
	return nil
}
func NewDatabaseCredentials(d DatabasePlan, password string) (DatabaseCredentials, error) {
	if err := (ServiceCatalog{Database: &d}).Validate(); err != nil {
		return DatabaseCredentials{}, err
	}
	if password != "" && !d.AllowCustomPassword {
		return DatabaseCredentials{}, errors.New("this profile requires a generated database password")
	}
	if password == "" {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			return DatabaseCredentials{}, errors.New("could not generate database password")
		}
		password = base64.RawURLEncoding.EncodeToString(b)
	}
	if len(password) < 16 || len(password) > 1024 || strings.ContainsAny(password, "\x00\r\n") {
		return DatabaseCredentials{}, errors.New("database password must contain 16–1024 characters without newlines")
	}
	user := d.Username
	if user == "" {
		user = "app"
	}
	// StackGres executes JDBC statements, not psql meta-commands. Database
	// creation must be a separate autocommit entry (it cannot run in a DO block).
	literal := func(value string) string {
		return "E'" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "''") + "'"
	}
	role := fmt.Sprintf("BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE \"%s\" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %s; END IF; END", user, user, literal(password))
	sql := "DO " + literal(role) + ";"
	databaseSQL := fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\";", d.DatabaseName, user)
	return DatabaseCredentials{Username: user, Password: password, SQL: sql, DatabaseSQL: databaseSQL}, nil
}

// Existing databases cannot be renamed or silently assigned new credentials during a profile upgrade.
func PreserveDatabaseCredentials(old Config, next *Config) error {
	a, e := resolvedServices(old)
	if e != nil {
		return e
	}
	b, e := resolvedServices(*next)
	if e != nil {
		return e
	}
	if a.Database == nil {
		return nil
	}
	if b.Database == nil {
		return errors.New("existing database cannot be removed by a profile update")
	}
	x, y := a.Database, b.Database
	if x.DatabaseName != y.DatabaseName || x.Username != y.Username || x.CredentialMode != y.CredentialMode || x.CredentialsSecret != y.CredentialsSecret || x.BootstrapSecret != y.BootstrapSecret || x.UsernameKey != y.UsernameKey || x.PasswordKey != y.PasswordKey || x.BootstrapKey != y.BootstrapKey {
		return errors.New("existing database name, user, credential mode and Secret references cannot change through a profile update")
	}
	return SetDatabaseCredentialID(next, x.CredentialID)
}
