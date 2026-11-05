package publishing

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	quantity "k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// Catalogs contain references only. Secret data and bootstrap SQL never enter Git.
type DatabasePlan struct {
	AllowEnvMapping     bool              `json:"allowEnvMapping,omitempty"`
	CredentialMode      string            `json:"credentialMode,omitempty"`
	CredentialID        string            `json:"credentialId,omitempty"`
	Username            string            `json:"username,omitempty"`
	AllowConfigure      bool              `json:"allowConfigure,omitempty"`
	AllowCustomPassword bool              `json:"allowCustomPassword,omitempty"`
	EnvNames            map[string]string `json:"envNames,omitempty"`
	Name                string            `json:"name"`
	PostgresVersion     string            `json:"postgresVersion"`
	Instances           int               `json:"instances"`
	InstanceProfile     string            `json:"instanceProfile,omitempty"`
	Storage             string            `json:"storage"`
	StorageClass        string            `json:"storageClass,omitempty"`
	BackupStorage       string            `json:"backupStorage,omitempty"`
	DatabaseName        string            `json:"databaseName"`
	CredentialsSecret   string            `json:"credentialsSecret"`
	UsernameKey         string            `json:"usernameKey"`
	PasswordKey         string            `json:"passwordKey"`
	BootstrapSecret     string            `json:"bootstrapSecret"`
	BootstrapKey        string            `json:"bootstrapKey"`
	RequiredVaultSecret string            `json:"requiredVaultSecret,omitempty"`
}
type SecretEnv struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}
type VaultBinding struct {
	AllowPathSuffix bool        `json:"allowPathSuffix,omitempty"`
	AllowEnvMapping bool        `json:"allowEnvMapping,omitempty"`
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	AuthRef         string      `json:"authRef"`
	VaultNamespace  string      `json:"vaultNamespace,omitempty"`
	Mount           string      `json:"mount"`
	Path            string      `json:"path"`
	Type            string      `json:"type"`
	RefreshAfter    string      `json:"refreshAfter"`
	Destination     string      `json:"destination"`
	Envs            []SecretEnv `json:"envs"`
}
type ServiceCatalog struct {
	AllowEnvironment bool           `json:"allowEnvironment,omitempty"`
	Database         *DatabasePlan  `json:"database,omitempty"`
	VaultSecrets     []VaultBinding `json:"vaultSecrets,omitempty"`
}
type DatabaseInput struct {
	Name     string            `json:"name,omitempty"`
	Username string            `json:"username,omitempty"`
	EnvNames map[string]string `json:"envNames,omitempty"`
}
type VaultInput struct {
	PathSuffix string       `json:"pathSuffix,omitempty"`
	Envs       *[]SecretEnv `json:"envs,omitempty"`
}
type LiteralEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type ServiceSelection struct {
	DatabaseConfig *DatabaseInput        `json:"databaseConfig,omitempty"`
	VaultConfig    map[string]VaultInput `json:"vaultConfig,omitempty"`
	Environment    []LiteralEnv          `json:"environment,omitempty"`
	Database       bool                  `json:"database"`
	VaultSecrets   []string              `json:"vaultSecrets,omitempty"`
}
type ResolvedServices struct {
	AllowEnvironment bool              `json:"allowEnvironment,omitempty"`
	Choices          *ServiceSelection `json:"choices,omitempty"`
	Environment      []LiteralEnv      `json:"environment,omitempty"`
	Database         *DatabasePlan     `json:"database,omitempty"`
	VaultSecrets     []VaultBinding    `json:"vaultSecrets,omitempty"`
}
type ServiceOption struct {
	AllowPathSuffix bool        `json:"allowPathSuffix,omitempty"`
	AllowEnvMapping bool        `json:"allowEnvMapping,omitempty"`
	Path            string      `json:"path,omitempty"`
	Envs            []SecretEnv `json:"envs,omitempty"`
	ID              string      `json:"id"`
	Name            string      `json:"name"`
}
type DatabaseOptions struct {
	Name                string            `json:"name"`
	DatabaseName        string            `json:"databaseName"`
	Username            string            `json:"username,omitempty"`
	CredentialMode      string            `json:"credentialMode,omitempty"`
	AllowConfigure      bool              `json:"allowConfigure"`
	AllowCustomPassword bool              `json:"allowCustomPassword"`
	AllowEnvMapping     bool              `json:"allowEnvMapping"`
	EnvNames            map[string]string `json:"envNames,omitempty"`
}
type ServiceOptions struct {
	AllowEnvironment    bool             `json:"allowEnvironment,omitempty"`
	Database            *DatabaseOptions `json:"database,omitempty"`
	DatabaseVaultSecret string           `json:"databaseVaultSecret,omitempty"`
	DatabaseName        string           `json:"databaseName,omitempty"`
	VaultSecrets        []ServiceOption  `json:"vaultSecrets"`
}

func decodeServices(text string, out any) error {
	if len(text) > 32<<10 {
		return errors.New("application service configuration exceeds 32 KiB")
	}
	if text == "" {
		return nil
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid application service configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid application service configuration")
	}
	return nil
}

var secretNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
var secretKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,253}$`)
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ExpandServiceReference(s, name, namespace string) string {
	return expandReference(s, name, namespace)
}
func expandReference(s, name, namespace string) string {
	return strings.NewReplacer("{app}", name, "{namespace}", namespace).Replace(s)
}
func validRef(s string) bool {
	return secretNamePattern.MatchString(expandReference(s, "sample-app", "sample-namespace"))
}
func ParseServiceCatalog(text string) (ServiceCatalog, error) {
	var c ServiceCatalog
	if err := decodeServices(text, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func (c ServiceCatalog) Validate() error {
	if len(c.VaultSecrets) > 10 {
		return errors.New("at most 10 Vault secret bindings are allowed")
	}
	ids, destinations := map[string]bool{}, map[string]bool{}
	for _, v := range c.VaultSecrets {
		if !namePattern.MatchString(v.ID) || len(v.ID) > 30 || ids[v.ID] || v.Name == "" || len(v.Name) > 100 {
			return errors.New("Vault binding IDs must be unique DNS labels and need a display name")
		}
		ids[v.ID] = true
		if !validRef(v.Destination) || destinations[v.Destination] {
			return errors.New("Vault destination Secret names must be valid and unique")
		}
		destinations[v.Destination] = true
		auth := strings.Split(v.AuthRef, "/")
		if len(auth) > 2 || len(auth) == 0 {
			return errors.New("invalid VaultAuth reference")
		}
		for _, p := range auth {
			if !validRef(p) {
				return errors.New("VaultAuth reference is required and must be a name or namespace/name")
			}
		}
		for _, p := range []string{v.Mount, v.Path} {
			p = expandReference(p, "sample-app", "sample-namespace")
			if !safePath(p) || p == "." || len(p) > 512 || !regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`).MatchString(p) {
				return errors.New("Vault mount and secret path must be relative paths; {app} and {namespace} placeholders are supported")
			}
		}
		if v.VaultNamespace != "" && (!safePath(v.VaultNamespace) || v.VaultNamespace == ".") {
			return errors.New("invalid Vault namespace")
		}
		if v.Type != "kv-v1" && v.Type != "kv-v2" {
			return errors.New("Vault secret type must be kv-v1 or kv-v2")
		}
		if !regexp.MustCompile(`^[1-9][0-9]*(s|m|h)$`).MatchString(v.RefreshAfter) {
			return errors.New("Vault refresh interval must be a positive duration such as 60s or 1h")
		}
		if len(v.Envs) > 30 {
			return errors.New("at most 30 environment mappings per secret")
		}
		seen := map[string]bool{}
		for _, e := range v.Envs {
			if !envNamePattern.MatchString(e.Name) || !secretKeyPattern.MatchString(e.Key) || seen[e.Name] {
				return errors.New("secret mappings require unique environment names and valid secret keys")
			}
			seen[e.Name] = true
		}
	}
	if d := c.Database; d != nil {
		if d.CredentialMode == "generated" && (d.DatabaseName == "postgres" || d.DatabaseName == "template0" || d.DatabaseName == "template1" || d.Username == "replicator" || d.Username == "authenticator" || d.Username == "patroni" || d.Username == "stackgres") {
			return errors.New("reserved database or operator role name")
		}
		if d.CredentialMode != "" && d.CredentialMode != "platform" && d.CredentialMode != "generated" {
			return errors.New("invalid database credential mode")
		}
		if d.CredentialMode == "generated" && (d.RequiredVaultSecret != "" || d.CredentialsSecret != d.BootstrapSecret || !strings.Contains(d.CredentialsSecret, "{app}")) {
			return errors.New("generated credentials require one dedicated Secret and no required Vault binding")
		}
		if d.CredentialMode == "generated" && (len(d.BootstrapKey) > 244 || d.UsernameKey == d.BootstrapKey+".database" || d.PasswordKey == d.BootstrapKey+".database") {
			return errors.New("bootstrap key must leave room for the generated database SQL key")
		}
		if d.AllowCustomPassword && d.CredentialMode != "generated" {
			return errors.New("custom database passwords require JustCD-managed credentials")
		}
		if d.CredentialMode == "generated" && (d.UsernameKey == d.PasswordKey || d.UsernameKey == d.BootstrapKey || d.PasswordKey == d.BootstrapKey) {
			return errors.New("database credential keys must be distinct")
		}
		if d.AllowConfigure && d.CredentialMode != "generated" {
			return errors.New("editable database names/users require JustCD-managed credentials and bootstrap SQL")
		}
		if d.Username != "" && (!regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(d.Username) || d.Username == "postgres" || strings.HasPrefix(d.Username, "pg_")) {
			return errors.New("application username must be a restricted lowercase SQL identifier")
		}
		for key, name := range d.EnvNames {
			if !databaseEnvKeys[key] || (name != "" && !envNamePattern.MatchString(name)) {
				return errors.New("invalid database environment mapping")
			}
		}

		if d.CredentialMode == "generated" {
			for _, v := range c.VaultSecrets {
				if v.Destination == d.CredentialsSecret {
					return errors.New("Vault cannot manage the JustCD-generated database Secret")
				}
			}
		}
		if d.Name == "" || len(d.Name) > 100 {
			return errors.New("database plan name is required")
		}
		if !regexp.MustCompile(`^[0-9]{2}(\.[0-9]+)?$`).MatchString(d.PostgresVersion) || d.Instances < 1 || d.Instances > 5 {
			return errors.New("database requires an explicit PostgreSQL version and 1–5 instances")
		}
		q, err := quantity.ParseQuantity(d.Storage)
		if err != nil || q.Sign() <= 0 {
			return errors.New("database storage must be a positive quantity such as 10Gi")
		}
		for _, ref := range []string{d.InstanceProfile, d.StorageClass, d.BackupStorage} {
			if ref != "" && !validRef(ref) {
				return errors.New("invalid database profile, storage class or backup reference")
			}
		}
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(d.DatabaseName) {
			return errors.New("database name must be a lowercase SQL identifier")
		}
		if !validRef(d.CredentialsSecret) || !validRef(d.BootstrapSecret) || !secretKeyPattern.MatchString(d.UsernameKey) || !secretKeyPattern.MatchString(d.PasswordKey) || !secretKeyPattern.MatchString(d.BootstrapKey) {
			return errors.New("database requires an application credentials Secret and bootstrap SQL Secret with valid keys")
		}
		if d.PasswordKey == "superuser-password" || d.UsernameKey == "superuser-username" {
			return errors.New("applications must use restricted database credentials, not StackGres superuser credentials")
		}
		if d.RequiredVaultSecret != "" && !ids[d.RequiredVaultSecret] {
			return errors.New("database credential binding must reference an approved Vault secret ID")
		}
	}
	return nil
}
func (c ServiceCatalog) Options() ServiceOptions {
	o := ServiceOptions{AllowEnvironment: c.AllowEnvironment, VaultSecrets: []ServiceOption{}}
	if c.Database != nil {
		o.DatabaseName = c.Database.Name
		d := *c.Database
		// Only expose user-facing defaults and permissions, never infrastructure refs.
		o.Database = &DatabaseOptions{Name: d.Name, DatabaseName: d.DatabaseName, Username: d.Username, CredentialMode: d.CredentialMode, AllowConfigure: d.AllowConfigure, AllowEnvMapping: d.AllowEnvMapping, AllowCustomPassword: d.AllowCustomPassword, EnvNames: d.EnvNames}
		o.DatabaseVaultSecret = c.Database.RequiredVaultSecret
	}
	for _, v := range c.VaultSecrets {
		o.VaultSecrets = append(o.VaultSecrets, ServiceOption{ID: v.ID, Name: v.Name, AllowPathSuffix: v.AllowPathSuffix, AllowEnvMapping: v.AllowEnvMapping, Path: v.Path, Envs: v.Envs})
	}
	return o
}
func (c ServiceCatalog) Resolve(selection ServiceSelection) (string, error) {
	r := ResolvedServices{Environment: selection.Environment}
	if len(selection.Environment) > 0 && !c.AllowEnvironment {
		return "", errors.New("this profile does not allow additional environment variables")
	}
	ids := map[string]bool{}
	for _, id := range selection.VaultSecrets {
		if ids[id] {
			return "", errors.New("duplicate Vault secret selection")
		}
		ids[id] = true
	}
	if selection.Database {
		if c.Database == nil {
			return "", errors.New("this profile does not offer a database")
		}
		d := *c.Database
		r.Database = &d
		if input := selection.DatabaseConfig; input != nil {
			if !d.AllowConfigure && (input.Name != "" || input.Username != "") {
				return "", errors.New("database name and username are locked by the profile")
			}
			if input.Name != "" {
				d.DatabaseName = input.Name
			}
			if input.Username != "" {
				d.Username = input.Username
			}
			if input.EnvNames != nil {
				if !d.AllowEnvMapping {
					return "", errors.New("database environment mappings are locked by the profile")
				}
				d.EnvNames = input.EnvNames
			}
		}
		if c.Database.RequiredVaultSecret != "" {
			ids[c.Database.RequiredVaultSecret] = true
		}
	}
	for _, v := range c.VaultSecrets {
		if ids[v.ID] {
			if input, ok := selection.VaultConfig[v.ID]; ok {
				if input.PathSuffix != "" {
					if !v.AllowPathSuffix || !safePath(input.PathSuffix) || input.PathSuffix == "." {
						return "", errors.New("Vault path suffix must stay within the approved path")
					}
					v.Path = strings.TrimRight(v.Path, "/") + "/" + input.PathSuffix
				}
				if input.Envs != nil {
					if !v.AllowEnvMapping {
						return "", errors.New("Vault environment mappings are locked by the profile")
					}
					v.Envs = *input.Envs
				}
			}
			r.VaultSecrets = append(r.VaultSecrets, v)
			delete(ids, v.ID)
		}
	}
	if len(ids) > 0 {
		return "", errors.New("select only approved Vault secret bindings")
	}
	for id := range selection.VaultConfig {
		found := false
		for _, v := range r.VaultSecrets {
			if v.ID == id {
				found = true
			}
		}
		if !found {
			return "", errors.New("configure only selected approved Vault bindings")
		}
	}
	if !selection.Database && selection.DatabaseConfig != nil {
		return "", errors.New("select a database before configuring it")
	}
	if err := validateServiceEnvironment(r); err != nil {
		return "", err
	}
	if err := (ServiceCatalog{Database: r.Database, VaultSecrets: r.VaultSecrets}).Validate(); err != nil {
		return "", err
	}
	if r.Database == nil && len(r.VaultSecrets) == 0 && len(r.Environment) == 0 {
		return "", nil
	}
	r.Choices = &selection
	b, _ := json.Marshal(r)
	return string(b), nil
}
func ServiceChoices(text string) (ServiceSelection, error) {
	var r ResolvedServices
	if err := decodeServices(text, &r); err != nil {
		return ServiceSelection{}, err
	}
	if r.Choices != nil {
		return *r.Choices, nil
	}
	s := ServiceSelection{Database: r.Database != nil}
	for _, v := range r.VaultSecrets {
		s.VaultSecrets = append(s.VaultSecrets, v.ID)
	}
	return s, nil
}
func resolvedServices(c Config) (ResolvedServices, error) {
	var r ResolvedServices
	if err := decodeServices(c.Services, &r); err != nil {
		return r, err
	}
	catalog := ServiceCatalog{Database: r.Database, VaultSecrets: r.VaultSecrets}
	if err := validateServiceEnvironment(r); err != nil {
		return r, err
	}
	return r, catalog.Validate()
}
func ServiceResourceName(name, suffix string) string {
	if len(name)+len(suffix) <= 63 {
		return name + suffix
	}
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%s-%x%s", name[:63-len(suffix)-8], sum[:3], suffix)
}
func serviceObjects(c Config) ([]map[string]any, []any, error) {
	r, err := resolvedServices(c)
	if err != nil {
		return nil, nil, err
	}
	objects := []map[string]any{}
	envs := []any{}
	envNames := map[string]bool{}
	meta := func(name string, retain bool) map[string]any {
		m := map[string]any{"name": name, "namespace": c.Namespace}
		if retain {
			m["annotations"] = map[string]any{"justcd.io/retain": "true"}
		}
		return m
	}
	reference := func(s string) string { return expandReference(s, c.Name, c.Namespace) }
	addEnv := func(name, key, secret, value string) error {
		if envNames[name] {
			return fmt.Errorf("duplicate service environment variable %s", name)
		}
		envNames[name] = true
		e := map[string]any{"name": name}
		if secret != "" {
			e["valueFrom"] = map[string]any{"secretKeyRef": map[string]any{"name": secret, "key": key}}
		} else {
			e["value"] = value
		}
		envs = append(envs, e)
		return nil
	}
	for _, e := range r.Environment {
		if err := addEnv(e.Name, "", "", e.Value); err != nil {
			return nil, nil, err
		}
	}
	requiredVault := ""
	if r.Database != nil {
		requiredVault = r.Database.RequiredVaultSecret
	}
	destinations := map[string]bool{}
	for _, v := range r.VaultSecrets {
		dest := reference(v.Destination)
		if destinations[dest] {
			return nil, nil, errors.New("expanded Vault destination Secret names must be unique")
		}
		destinations[dest] = true
		if r.Database != nil && r.Database.CredentialMode == "generated" && dest == reference(r.Database.CredentialsSecret) {
			return nil, nil, errors.New("Vault destination conflicts with the generated database Secret")
		}
		if !secretNamePattern.MatchString(dest) {
			return nil, nil, errors.New("expanded Vault destination name is invalid")
		}
		spec := map[string]any{"vaultAuthRef": reference(v.AuthRef), "mount": reference(v.Mount), "path": reference(v.Path), "type": v.Type, "refreshAfter": v.RefreshAfter, "hmacSecretData": true, "destination": map[string]any{"create": true, "name": dest}, "rolloutRestartTargets": []any{map[string]any{"kind": "Deployment", "name": c.Name}}}
		if v.VaultNamespace != "" {
			spec["namespace"] = v.VaultNamespace
		}
		objects = append(objects, map[string]any{"apiVersion": "secrets.hashicorp.com/v1beta1", "kind": "VaultStaticSecret", "metadata": meta(ServiceResourceName(c.Name, "-vault-"+v.ID), v.ID == requiredVault || (r.Database != nil && (v.Destination == r.Database.CredentialsSecret || v.Destination == r.Database.BootstrapSecret))), "spec": spec})
		for _, e := range v.Envs {
			if err := addEnv(e.Name, e.Key, dest, ""); err != nil {
				return nil, nil, err
			}
		}
	}
	if d := r.Database; d != nil {
		db := ServiceResourceName(c.Name, "-db")
		script := ServiceResourceName(c.Name, "-db-init")
		for _, ref := range []string{d.CredentialsSecret, d.BootstrapSecret} {
			if !secretNamePattern.MatchString(reference(ref)) || reference(ref) == db {
				return nil, nil, errors.New("database Secret reference is invalid or refers to StackGres superuser credentials")
			}
		}
		spec := map[string]any{"metadata": map[string]any{"labels": map[string]any{"clusterPods": map[string]any{"app": db}}}, "instances": d.Instances, "postgres": map[string]any{"version": d.PostgresVersion}, "pods": map[string]any{"persistentVolume": map[string]any{"size": d.Storage}}, "managedSql": map[string]any{"scripts": []any{map[string]any{"sgScript": script}}}}
		if d.InstanceProfile != "" {
			spec["sgInstanceProfile"] = reference(d.InstanceProfile)
		}
		if d.StorageClass != "" {
			spec["pods"].(map[string]any)["persistentVolume"].(map[string]any)["storageClass"] = reference(d.StorageClass)
		}
		if d.BackupStorage != "" {
			spec["configurations"] = map[string]any{"backups": []any{map[string]any{"sgObjectStorage": reference(d.BackupStorage)}}}
		}
		scripts := []any{map[string]any{"name": "application-role", "retryOnError": true, "scriptFrom": map[string]any{"secretKeyRef": map[string]any{"name": reference(d.BootstrapSecret), "key": d.BootstrapKey}}}}
		if d.CredentialMode == "generated" {
			scripts = append(scripts, map[string]any{"name": "application-database", "retryOnError": false, "scriptFrom": map[string]any{"secretKeyRef": map[string]any{"name": reference(d.BootstrapSecret), "key": d.BootstrapKey + ".database"}}})
		}
		objects = append(objects, map[string]any{"apiVersion": "stackgres.io/v1", "kind": "SGScript", "metadata": meta(script, true), "spec": map[string]any{"managedVersions": true, "continueOnError": false, "scripts": scripts}}, map[string]any{"apiVersion": "stackgres.io/v1", "kind": "SGCluster", "metadata": meta(db, true), "spec": spec})
		for _, e := range []struct{ name, key, value string }{{"DB_HOST", "", db + "." + c.Namespace + ".svc"}, {"DB_PORT", "", "5432"}, {"DB_NAME", "", d.DatabaseName}, {"DB_USER", d.UsernameKey, ""}, {"DB_PASSWORD", d.PasswordKey, ""}} {
			secret := ""
			if e.key != "" {
				secret = reference(d.CredentialsSecret)
			}
			name := e.name
			if custom, ok := d.EnvNames[e.name]; ok {
				name = custom
			}
			if name == "" {
				continue
			}
			if err := addEnv(name, e.key, secret, e.value); err != nil {
				return nil, nil, err
			}
		}
	}
	return objects, envs, nil
}

// AddServices works with built-in and custom templates without changing their source.
func AddServices(c Config, out map[string]string) error {
	if c.Services == "" {
		return nil
	}
	objects, envs, err := serviceObjects(c)
	if err != nil {
		return err
	}
	docs, err := parseDocuments(out[FileDeployment])
	if err != nil {
		return err
	}
	var combined bytes.Buffer
	for _, doc := range docs {
		if doc.kind == "Deployment" {
			containers, _ := dig(doc.body, "spec", "template", "spec", "containers").([]any)
			for _, ctr := range containers {
				m, _ := ctr.(map[string]any)
				if m["name"] == "app" {
					existing, _ := m["env"].([]any)
					for _, e := range existing {
						for _, added := range envs {
							if dig(e, "name") == dig(added, "name") {
								return fmt.Errorf("service environment variable %v conflicts with the template", dig(e, "name"))
							}
						}
					}
					m["env"] = append(existing, envs...)
				}
			}
		}
		b, err := yaml.Marshal(doc.body)
		if err != nil {
			return err
		}
		combined.Write(b)
		combined.WriteString("---\n")
	}
	for _, obj := range objects {
		b, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		combined.Write(b)
		combined.WriteString("---\n")
	}
	if combined.Len() > maxRenderedBytes {
		return errOutputLimit
	}
	out[FileDeployment] = combined.String()
	return nil
}
func validateServiceObject(c Config, r resource) error {
	objects, _, err := serviceObjects(c)
	if err != nil {
		return err
	}
	actual, _ := json.Marshal(r.body)
	for _, o := range objects {
		if o["kind"] == r.kind && dig(o, "metadata", "name") == r.name {
			expected, _ := json.Marshal(o)
			var normalized map[string]any
			_ = json.Unmarshal(expected, &normalized)
			expected, _ = json.Marshal(normalized)
			if !bytes.Equal(actual, expected) {
				return errors.New("service resources must match the approved application service configuration")
			}
			return nil
		}
	}
	return errors.New("operator resource is not part of the approved application services")
}

var databaseEnvKeys = map[string]bool{"DB_HOST": true, "DB_PORT": true, "DB_NAME": true, "DB_USER": true, "DB_PASSWORD": true}

func validateServiceEnvironment(r ResolvedServices) error {
	if len(r.Environment) > 30 {
		return errors.New("at most 30 additional environment variables")
	}
	seen := map[string]bool{}
	for _, e := range r.Environment {
		if !envNamePattern.MatchString(e.Name) || seen[e.Name] || len(e.Value) > 4096 {
			return errors.New("additional environment variables require unique names and values up to 4096 characters")
		}
		seen[e.Name] = true
	}
	return nil
}
