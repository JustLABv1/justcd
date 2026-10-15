package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestAuditFilterValidation(t *testing.T) {
	for _, query := range []string{"before=0", "before=no", "eventId=-1", "group=no", "from=2026-01-01", "from=2026-02-01T00:00:00Z&until=2026-01-01T00:00:00Z", "q=" + strings.Repeat("a", 501)} {
		if _, err := parseAuditFilter(httptest.NewRequest("GET", "/?"+query, nil)); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
	f, err := parseAuditFilter(httptest.NewRequest("GET", "/?workspaceId=w&group=deployments&eventId=42&before=50&from=2026-01-01T00:00:00Z", nil))
	if err != nil || f.EventID != 42 || f.Before != 50 || f.WorkspaceID != "w" || f.From == nil {
		t.Fatalf("valid filter lost: %+v %v", f, err)
	}
}
func TestAuditExportVersionRedactionAndChecksum(t *testing.T) {
	actor := "user-1"
	e := store.AuditEvent{SchemaVersion: 1, ID: 42, ActorID: &actor, Action: "sync.succeeded", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Details: json.RawMessage(`{"planId":"p","operationId":"o","settings":{"password":"never-export"},"manifest":{"kind":"Secret","data":{"key":"never-export"}},"message":"never-export","unknown":"never-export","changes":[{"kind":"Secret","data":{"key":"never-export"}}]}`)}
	record := exportAuditRecord(e)
	raw, err := json.Marshal(record)
	if err != nil || strings.Contains(string(raw), "never-export") {
		t.Fatalf("unsafe export %s %v", raw, err)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&value)
	delete(value, "sha256")
	canonical, _ := json.Marshal(value)
	sum := sha256.Sum256(canonical)
	if record.SHA256 != hex.EncodeToString(sum[:]) || record.SchemaVersion != 1 {
		t.Fatalf("incompatible checksum/schema: %s", raw)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, key := range []string{"schemaVersion", "id", "actorId", "actorName", "workspaceId", "resourceId", "resourceType", "details", "createdAt", "deploymentRecord", "sha256"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("schema field missing: %s", key)
		}
	}
}
func TestAuditCSVFormulaProtection(t *testing.T) {
	for _, v := range []string{"=HYPERLINK(\"bad\")", " +cmd", "\t@cmd", "\r-cmd", "-10"} {
		if !strings.HasPrefix(csvAuditValue(v), "'") {
			t.Errorf("formula not neutralized: %q", v)
		}
	}
	if csvAuditValue("normal") != "normal" {
		t.Fatal("changed safe value")
	}
}
func TestAuditUnscopedNonAdminDenied(t *testing.T) {
	server := &Server{}
	for _, handler := range []http.HandlerFunc{server.listAudit, server.exportAudit} {
		r := httptest.NewRequest("GET", "/api/v1/audit", nil)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: "viewer"}))
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != 403 {
			t.Fatalf("unscoped access: %d", w.Code)
		}
	}
}
