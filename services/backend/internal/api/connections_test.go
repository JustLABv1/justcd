package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestGitHTTPSUsernameIsVisibleWithoutToken(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	server := &Server{EncryptionKey: key}
	credential := store.Credential{ID: "git-credential", Kind: "git-https", Name: "private repo"}
	var err error
	credential.Cipher, err = security.Encrypt(key, []byte(`{"username":"deploy-user","token":"top-secret-token"}`), "credential:"+credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	credential.Username, err = server.gitHTTPSUsername(credential)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Username != "deploy-user" {
		t.Fatalf("username = %q", credential.Username)
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret-token") || strings.Contains(string(encoded), "secretCipher") {
		t.Fatalf("secret leaked: %s", encoded)
	}
}

func TestGitHTTPSUsernameLegacyFallback(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	server := &Server{EncryptionKey: key}
	credential := store.Credential{ID: "old-git-credential", Kind: "git-https"}
	var err error
	credential.Cipher, err = security.Encrypt(key, []byte(`{"token":"legacy-token"}`), "credential:"+credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	username, err := server.gitHTTPSUsername(credential)
	if err != nil {
		t.Fatal(err)
	}
	if username != "justcd" {
		t.Fatalf("legacy username = %q", username)
	}
}
