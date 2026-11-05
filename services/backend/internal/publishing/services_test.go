package publishing

import (
	"encoding/json"
	"strings"
	"testing"
)

func serviceTestCatalog() ServiceCatalog {
	return ServiceCatalog{Database: &DatabasePlan{Name: "Development PostgreSQL", PostgresVersion: "17", Instances: 1, Storage: "10Gi", DatabaseName: "app", CredentialsSecret: "{app}-database", UsernameKey: "username", PasswordKey: "password", BootstrapSecret: "{app}-database", BootstrapKey: "init.sql", RequiredVaultSecret: "database"}, VaultSecrets: []VaultBinding{
		{ID: "database", Name: "Database credentials", AuthRef: "platform/apps", Mount: "secret", Path: "apps/{app}/database", Type: "kv-v2", RefreshAfter: "1m", Destination: "{app}-database"},
		{ID: "s3", Name: "S3 credentials", AuthRef: "platform/apps", Mount: "secret", Path: "apps/{app}/s3", Type: "kv-v2", RefreshAfter: "1m", Destination: "{app}-s3", Envs: []SecretEnv{{Name: "S3_ACCESS_KEY", Key: "access_key"}, {Name: "S3_SECRET_KEY", Key: "secret_key"}}},
	}}
}
func TestApplicationServicesOptionalCombinations(t *testing.T) {
	catalog := serviceTestCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []ServiceSelection{{}, {Database: true}, {VaultSecrets: []string{"s3"}}, {Database: true, VaultSecrets: []string{"s3"}}} {
		c, _ := SampleConfig("shiny")
		var err error
		c.Services, err = catalog.Resolve(selection)
		if err != nil {
			t.Fatal(err)
		}
		files, err := RenderConfig(c, SourceFacts{})
		if err != nil {
			t.Fatal(err)
		}
		manifest := files[FileDeployment]
		if strings.Contains(manifest, "kind: SGCluster") != selection.Database {
			t.Fatal("database selection ignored")
		}
		if strings.Contains(manifest, "S3_SECRET_KEY") != (len(selection.VaultSecrets) > 0) {
			t.Fatal("Vault selection ignored")
		}
		if selection.Database {
			for _, want := range []string{"kind: SGScript", "kind: VaultStaticSecret", "justcd.io/retain", "DB_PASSWORD", "secretKeyRef:", "sample-app-database", "scriptFrom:", "init.sql"} {
				if !strings.Contains(manifest, want) {
					t.Fatalf("missing %s", want)
				}
			}
			if strings.Contains(manifest, "superuser-password") || strings.Contains(manifest, "CREATE USER") {
				t.Fatal("bootstrap credential material entered Git")
			}
		}
		choices, err := ServiceChoices(c.Services)
		if err != nil || choices.Database != selection.Database {
			t.Fatal(choices, err)
		}
	}
}
func TestServiceValidationAndBoundaries(t *testing.T) {
	cases := map[string]func(*ServiceCatalog){
		"bad storage":                func(c *ServiceCatalog) { c.Database.Storage = "-5Gi" },
		"superuser key":              func(c *ServiceCatalog) { c.Database.PasswordKey = "superuser-password" },
		"missing credential binding": func(c *ServiceCatalog) { c.Database.RequiredVaultSecret = "foreign" },
		"path traversal":             func(c *ServiceCatalog) { c.VaultSecrets[0].Path = "../admin" },
		"destination traversal":      func(c *ServiceCatalog) { c.VaultSecrets[0].Destination = "../bad" },
		"missing auth":               func(c *ServiceCatalog) { c.VaultSecrets[0].AuthRef = "" },
		"invalid environment":        func(c *ServiceCatalog) { c.VaultSecrets[1].Envs[0].Name = "INVALID-NAME" },
		"duplicate bindings":         func(c *ServiceCatalog) { c.VaultSecrets[1].ID = "database" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := serviceTestCatalog()
			mutate(&c)
			if c.Validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	catalog := serviceTestCatalog()
	if _, err := catalog.Resolve(ServiceSelection{VaultSecrets: []string{"not-approved"}}); err == nil {
		t.Fatal("unapproved binding accepted")
	}
	if _, err := (ServiceCatalog{}).Resolve(ServiceSelection{Database: true}); err == nil {
		t.Fatal("database enabled without approved plan")
	}
	c, _ := SampleConfig("streamlit")
	c.Services, _ = catalog.Resolve(ServiceSelection{Database: true})
	files, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(files[FileDeployment], "size: 10Gi", "size: 1000Gi", 1)
	if validateManifest(c, forged) == nil {
		t.Fatal("database plan override accepted")
	}
}
func TestServicesArePreservedByConfigRoundTrip(t *testing.T) {
	catalog := serviceTestCatalog()
	s, err := catalog.Resolve(ServiceSelection{Database: true, VaultSecrets: []string{"s3"}})
	if err != nil {
		t.Fatal(err)
	}
	before := Config{Services: s}
	b, _ := json.Marshal(before)
	var after Config
	if err := json.Unmarshal(b, &after); err != nil || before != after {
		t.Fatal(after, err)
	}
	// Long DNS names must still produce valid, distinct operator resource names.
	for _, suffix := range []string{"-db", "-db-init", "-vault-database"} {
		name := ServiceResourceName(strings.Repeat("a", 63), suffix)
		if len(name) > 63 || !namePattern.MatchString(name) {
			t.Fatal(name)
		}
	}
}

func TestExpandedVaultDestinationsCannotCollide(t *testing.T) {
	catalog := serviceTestCatalog()
	catalog.VaultSecrets[1].Destination = "sample-app-database"
	c, _ := SampleConfig("shiny")
	var err error
	c.Services, err = catalog.Resolve(ServiceSelection{Database: true, VaultSecrets: []string{"s3"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RenderConfig(c, SourceFacts{}); err == nil {
		t.Fatal("colliding expanded Secret destinations accepted")
	}
}

func configurableCatalog() ServiceCatalog {
	c := serviceTestCatalog()
	c.AllowEnvironment = true
	c.Database.CredentialMode = "generated"
	c.Database.RequiredVaultSecret = ""
	c.Database.Username = "app"
	c.Database.AllowConfigure = true
	c.Database.AllowCustomPassword = true
	c.Database.AllowEnvMapping = true
	c.VaultSecrets = c.VaultSecrets[1:]
	c.VaultSecrets[0].AllowEnvMapping = true
	c.VaultSecrets[0].AllowPathSuffix = true
	return c
}
func TestUserServiceParametersRespectProfileBoundaries(t *testing.T) {
	c := configurableCatalog()
	envs := []SecretEnv{{Name: "AWS_SECRET_ACCESS_KEY", Key: "secret_key"}}
	selection := ServiceSelection{Database: true, DatabaseConfig: &DatabaseInput{Name: "reports", Username: "report_user", EnvNames: map[string]string{"DB_HOST": "PGHOST", "DB_PASSWORD": "PGPASSWORD"}}, VaultSecrets: []string{"s3"}, VaultConfig: map[string]VaultInput{"s3": {PathSuffix: "team/reports", Envs: &envs}}, Environment: []LiteralEnv{{Name: "APP_THEME", Value: "dark"}}}
	resolved, err := c.Resolve(selection)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := SampleConfig("streamlit")
	cfg.Services = resolved
	files, err := RenderConfig(cfg, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PGHOST", "PGPASSWORD", "reports", "AWS_SECRET_ACCESS_KEY", "team/reports", "APP_THEME"} {
		if !strings.Contains(files[FileDeployment], want) {
			t.Fatalf("missing %s", want)
		}
	}
	choices, err := ServiceChoices(resolved)
	if err != nil || choices.DatabaseConfig.Name != "reports" || choices.VaultConfig["s3"].PathSuffix != "team/reports" {
		t.Fatal("app configuration not preserved", err)
	}
	for _, suffix := range []string{"../other", "/other", "x/../../other", "x/../other"} {
		selection.VaultConfig["s3"] = VaultInput{PathSuffix: suffix}
		if _, err := c.Resolve(selection); err == nil {
			t.Fatalf("escaped Vault boundary %s", suffix)
		}
	}
	selection.VaultConfig = nil
	c.Database.AllowConfigure = false
	if _, err := c.Resolve(selection); err == nil {
		t.Fatal("locked database overridden")
	}
	c.Database.AllowConfigure = true
	c.Database.AllowEnvMapping = false
	if _, err := c.Resolve(selection); err == nil {
		t.Fatal("locked database mappings overridden")
	}
	c.Database.AllowEnvMapping = true
	c.AllowEnvironment = false
	if _, err := c.Resolve(selection); err == nil {
		t.Fatal("additional environment policy ignored")
	}
	c.AllowEnvironment = true
	c.VaultSecrets[0].AllowEnvMapping = false
	selection.VaultConfig = map[string]VaultInput{"s3": {Envs: &envs}}
	if _, err := c.Resolve(selection); err == nil {
		t.Fatal("locked Vault mappings overridden")
	}
}
func TestManagedDatabaseCredentialsStayOutsideGeneratedFiles(t *testing.T) {
	catalog := configurableCatalog()
	cfg, _ := SampleConfig("shiny")
	cfg.Services, _ = catalog.Resolve(ServiceSelection{Database: true, DatabaseConfig: &DatabaseInput{Name: "reports", Username: "report_user"}})
	d, err := ManagedDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	values, err := NewDatabaseCredentials(*d, "password-with-'quotes-and\\slashes")
	if err != nil || values.Username != "report_user" || !strings.Contains(values.SQL, "NOSUPERUSER NOCREATEDB NOCREATEROLE") || !strings.Contains(values.DatabaseSQL, `CREATE DATABASE "reports" OWNER "report_user"`) {
		t.Fatal("invalid bootstrap", err)
	}
	if !strings.Contains(values.SQL, "quotes") {
		t.Fatal("password literal not escaped")
	}
	generated, err := NewDatabaseCredentials(*d, "")
	if err != nil || len(generated.Password) < 32 || generated.Password == values.Password {
		t.Fatal("password generation failed", err)
	}
	SetDatabaseCredentialID(&cfg, "protected-ref")
	files, err := RenderConfig(cfg, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	serialized, _ := json.Marshal(cfg)
	if strings.Contains(string(serialized), values.Password) || strings.Contains(files[FileDeployment], values.Password) || strings.Contains(files[FileDeployment], "CREATE ROLE") {
		t.Fatal("secret data leaked into Git/config")
	}
	next := cfg
	if err := PreserveDatabaseCredentials(cfg, &next); err != nil {
		t.Fatal(err)
	}
	next.Services = strings.Replace(next.Services, `"databaseName":"reports"`, `"databaseName":"other"`, 1)
	if err := PreserveDatabaseCredentials(cfg, &next); err == nil {
		t.Fatal("database rename accepted during upgrade")
	}
}
