//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSecurityHardening(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	hash, err := security.HashPassword("correct-horse-battery-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, `INSERT INTO users(id,email,password_hash) VALUES('local','local@example.invalid',$1),('other','other@example.invalid',$1)`, hash); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	cfg := config.Config{EncryptionKey: key, SessionLifetime: time.Hour, FrontendURL: "https://justcd.example", PublicURL: "https://justcd.example", SessionCookieSecure: true, Production: true}
	s, err := New(db, cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	replica, err := New(&store.Store{DB: db.DB}, cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	token := "initial"
	csrf := "csrf"
	if err = db.CreateSession(ctx, security.HashToken(token), security.HashToken(csrf), "local", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	req := func(server *Server, method, path, body string, withCSRF bool, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		if withCSRF {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body)
		}
	}
	input := `{"password":"correct-horse-battery-password"}`
	expect(req(s, "POST", "/api/v1/auth/mfa/enroll", input, false, ""), 403)
	expect(req(s, "POST", "/api/v1/auth/mfa/enroll", input, true, "https://evil.example"), 403)
	w := req(s, "POST", "/api/v1/auth/mfa/enroll", input, true, cfg.FrontendURL)
	expect(w, 200)
	var enrollment struct{ Secret string }
	_ = json.Unmarshal(w.Body.Bytes(), &enrollment)
	code, _ := security.TOTP(enrollment.Secret, time.Now().Unix()/30)
	expect(req(s, "POST", "/api/v1/auth/mfa/confirm", `{"password":"correct-horse-battery-password","code":"000000"}`, true, ""), 401)
	w = req(s, "POST", "/api/v1/auth/mfa/confirm", fmt.Sprintf(`{"password":"correct-horse-battery-password","code":%q}`, code), true, "")
	expect(w, 200)
	var recovery struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &recovery)
	if len(recovery.RecoveryCodes) != 10 {
		t.Fatal("missing recovery codes")
	}
	// Enrollment kills sessions on the other replica before the next request.
	expect(req(replica, "GET", "/api/v1/auth/session", "", false, ""), 401)
	for _, c := range w.Result().Cookies() {
		if !c.Secure || c.SameSite != http.SameSiteStrictMode {
			t.Fatal("cookie policy")
		}
		if c.Name == sessionCookieName {
			if !c.HttpOnly || c.Value == token {
				t.Fatal("fixation")
			}
			token = c.Value
		}
		if c.Name == csrfCookieName {
			csrf = c.Value
		}
	}
	expect(req(replica, "GET", "/api/v1/auth/session", "", false, ""), 200)
	login := `{"email":"local@example.invalid","password":"correct-horse-battery-password"}`
	w = req(s, "POST", "/api/v1/auth/login", login, false, "")
	expect(w, 200)
	if len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), "mfaRequired") {
		t.Fatal("challenge issued authenticated cookies")
	}
	w = req(replica, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":"local@example.invalid","password":"correct-horse-battery-password","code":%q}`, code), false, "")
	expect(w, 401) // replay of enrollment code
	codeInput := fmt.Sprintf(`{"email":"local@example.invalid","password":"correct-horse-battery-password","code":%q}`, recovery.RecoveryCodes[0])
	w = req(replica, "POST", "/api/v1/auth/login", codeInput, false, "")
	expect(w, 200)
	expect(req(s, "POST", "/api/v1/auth/login", codeInput, false, ""), 401)
	// Session IDs expose neither session token nor its hash; another user's IDs cannot revoke it.
	w = req(s, "GET", "/api/v1/auth/sessions", "", false, "")
	expect(w, 200)
	var list struct{ Items []store.ActiveSession }
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 2 {
		t.Fatal("session list", w.Body)
	}
	other := list.Items[0]
	if other.Current {
		other = list.Items[1]
	}
	expect(req(s, "DELETE", "/api/v1/auth/sessions/"+other.ID, "", false, ""), 403)
	expect(req(s, "DELETE", "/api/v1/auth/sessions/"+other.ID, "", true, ""), 200)
	var otherID string
	if err = db.CreateSession(ctx, security.HashToken("other"), security.HashToken("othercsrf"), "other", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = db.DB.QueryRowContext(ctx, `SELECT id FROM sessions WHERE user_id='other'`).Scan(&otherID)
	expect(req(s, "DELETE", "/api/v1/auth/sessions/"+otherID, "", true, ""), 404)
	// Invalid login attempts are equivalent for existing and unknown accounts and shared across replicas.
	for _, email := range []string{"other@example.invalid", "unknown@example.invalid"} {
		body := fmt.Sprintf(`{"email":%q,"password":"wrong"}`, email)
		for i := 0; i < 8; i++ {
			server := s
			if i%2 == 1 {
				server = replica
			}
			expect(req(server, "POST", "/api/v1/auth/login", body, false, ""), 401)
		}
		w = req(replica, "POST", "/api/v1/auth/login", body, false, "")
		expect(w, 429)
		if w.Header().Get("Retry-After") == "" {
			t.Fatal("missing rate limit metadata")
		}
	}
	// Test endpoints and OIDC callbacks cannot evade the shared limiter.
	for i := 0; i < 10; i++ {
		allowed, err := db.AllowSecurityAttempt(ctx, "credential-test:local", 10, time.Minute)
		if err != nil || !allowed {
			t.Fatal(err)
		}
	}
	expect(req(s, "POST", "/api/v1/clusters/nonexistent/test", "{}", true, ""), 429)
	for i := 0; i < 60; i++ {
		allowed, err := db.AllowSecurityAttempt(ctx, "oidc:192.0.2.1", 60, time.Minute)
		if err != nil || !allowed {
			t.Fatal(err)
		}
	}
	expect(req(replica, "GET", "/api/v1/auth/oidc/missing/callback", "", false, ""), 429)
	remove := fmt.Sprintf(`{"password":"correct-horse-battery-password","code":%q}`, recovery.RecoveryCodes[1])
	expect(req(s, "DELETE", "/api/v1/auth/mfa", remove, true, ""), 200)
	expect(req(replica, "GET", "/api/v1/auth/session", "", false, ""), 401)
	for i := 0; i < 120; i++ {
		_, _ = db.AllowSecurityAttempt(ctx, "auth-ip:192.0.2.1", 120, 15*time.Minute)
	}
	expect(req(s, "POST", "/api/v1/auth/signup", `{}`, false, ""), 429)
	var audits int
	_ = db.DB.QueryRowContext(ctx, `SELECT count(DISTINCT action) FROM audit_events WHERE action LIKE 'mfa.%'`).Scan(&audits)
	if audits < 6 {
		t.Fatal("MFA audit coverage", audits)
	}
}
func TestIntegrationEncryptionRotation(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	old := []byte(strings.Repeat("o", 32))
	newKey := []byte(strings.Repeat("n", 32))
	cipher, err := security.Encrypt(old, []byte("TOP-SECRET"), "credential:rotate")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO credentials(id,name,kind,secret_cipher) VALUES('rotate','Rotate','git-https',$1)`, cipher)
	if err != nil {
		t.Fatal(err)
	}
	// MFA seeds are purpose-bound encrypted values and rotate with provider credentials.
	seedCipher, err := security.Encrypt(old, []byte("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"), "mfa:rotation-user")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('rotation-user','rotation@example.invalid');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO user_mfa(user_id,secret_cipher,enabled) VALUES('rotation-user',$1,TRUE)`, seedCipher)
	if err != nil {
		t.Fatal(err)
	}
	// A bad value later in the rotation aborts all previously re-encrypted values.
	_, err = db.DB.ExecContext(ctx, `INSERT INTO oidc_providers(id,name,issuer,client_id,client_secret_cipher,redirect_url) VALUES('bad','Bad','https://example.invalid','id','bad','https://example.invalid/callback')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RotateEncryptionKey(ctx, old, newKey); err == nil {
		t.Fatal("partial rotation accepted")
	}
	var stored []byte
	_ = db.DB.QueryRowContext(ctx, `SELECT secret_cipher FROM credentials WHERE id='rotate'`).Scan(&stored)
	if string(stored) != string(cipher) {
		t.Fatal("transaction did not roll back")
	}
	_, _ = db.DB.ExecContext(ctx, `DELETE FROM oidc_providers WHERE id='bad'`)
	count, err := db.RotateEncryptionKey(ctx, old, newKey)
	if err != nil || count != 2 {
		t.Fatalf("rotation: %d %v", count, err)
	}
	_ = db.DB.QueryRowContext(ctx, `SELECT secret_cipher FROM credentials WHERE id='rotate'`).Scan(&stored)
	plain, err := security.Decrypt(newKey, stored, "credential:rotate")
	if err != nil || string(plain) != "TOP-SECRET" {
		t.Fatal("new key failed", err)
	}
	var rotatedSeed []byte
	_ = db.DB.QueryRowContext(ctx, `SELECT secret_cipher FROM user_mfa WHERE user_id='rotation-user'`).Scan(&rotatedSeed)
	if _, err = security.Decrypt(newKey, rotatedSeed, "mfa:rotation-user"); err != nil {
		t.Fatal("MFA seed rotation", err)
	}
	if _, err = security.Decrypt(old, stored, "credential:rotate"); err == nil {
		t.Fatal("old key accepted")
	}
	if _, err = db.RotateEncryptionKey(ctx, newKey, old); err != nil {
		t.Fatal("rollback rotation failed", err)
	}
}
