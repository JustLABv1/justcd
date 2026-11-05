package syncer

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDatabaseSecretProvisioningIsIdempotentAndNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	secrets := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("apps")
	d := publishing.DatabasePlan{CredentialsSecret: "{app}-database", UsernameKey: "username", PasswordKey: "password", BootstrapKey: "init.sql"}
	values := publishing.DatabaseCredentials{Username: "reports", Password: "private-password-canary", SQL: "private-sql-canary"}
	provision := func() error {
		return ensureDatabaseSecret(ctx, secrets, "report", "apps", "app-id", d, "protected-id", values)
	}
	if err := provision(); err != nil {
		t.Fatal(err)
	}
	secret, err := secrets.Get(ctx, "report-database", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if secret.GetAnnotations()["justcd.io/retain"] != "true" {
		t.Fatal("credentials not retained")
	}
	data := secret.Object["data"].(map[string]any)
	if data["password"] != base64.StdEncoding.EncodeToString([]byte(values.Password)) {
		t.Fatal("wrong secret values")
	}
	if err := provision(); err != nil {
		t.Fatal("retry failed", err)
	}
	data["password"] = base64.StdEncoding.EncodeToString([]byte("externally-rotated"))
	secret.Object["data"] = data
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := provision(); err == nil || strings.Contains(err.Error(), values.Password) {
		t.Fatal("changed credentials overwritten or exposed")
	}
	secret.SetAnnotations(map[string]string{})
	secrets.Update(ctx, secret, metav1.UpdateOptions{})
	if err := provision(); err == nil {
		t.Fatal("unmanaged secret adopted")
	}
}
func TestDatabaseSecretErrorsDoNotExposePayload(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("create", "secrets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("private-password-canary private-sql-canary")
	})
	err := ensureDatabaseSecret(context.Background(), client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("apps"), "report", "apps", "app-id", publishing.DatabasePlan{CredentialsSecret: "{app}-database", PasswordKey: "password"}, "id", publishing.DatabaseCredentials{Password: "private-password-canary", SQL: "private-sql-canary"})
	if err == nil || strings.Contains(err.Error(), "canary") {
		t.Fatal("provider error leaked secret data")
	}
}
