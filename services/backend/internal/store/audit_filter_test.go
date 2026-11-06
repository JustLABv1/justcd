package store

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
)

func parseFilter(t *testing.T, raw string) *AuditCondition {
	t.Helper()
	condition, err := ParseAuditCondition(raw)
	if err != nil {
		t.Fatalf("ParseAuditCondition(%s): %v", raw, err)
	}
	return condition
}

func TestAuditConditionBuildsParameterisedSQL(t *testing.T) {
	condition := parseFilter(t, `{"type":"group","combinator":"and","rules":[
		{"type":"rule","field":"action","operator":"contains","value":"plan"},
		{"type":"group","combinator":"or","rules":[
			{"type":"rule","field":"actor","operator":"is","value":"justin"},
			{"type":"rule","field":"actorKind","operator":"is_any_of","value":["automation","people"]}]},
		{"type":"rule","field":"cluster","operator":"not_contains","value":"prod"}]}`)
	args := make([]any, 12)
	sql := buildAuditPredicate(condition, &args)
	for _, want := range []string{" AND ", " OR ", "ILIKE", "ANY($15::text[])", "NOT EXISTS"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("predicate lacks %q: %s", want, sql)
		}
	}
	if len(args) != 12+4 || args[12] != "plan" {
		t.Fatalf("bind values = %#v", args[12:])
	}
	if strings.Contains(sql, "plan") || strings.Contains(sql, "justin") || strings.Contains(sql, "prod") {
		t.Fatalf("values must be bound, not interpolated: %s", sql)
	}
}

func TestAuditConditionEscapesLikeWildcards(t *testing.T) {
	args := []any{}
	buildAuditPredicate(parseFilter(t, `{"type":"rule","field":"resourceName","operator":"contains","value":"50%_a\\b"}`), &args)
	if args[0] != `50\%\_a\\b` {
		t.Fatalf("wildcards were not escaped: %q", args[0])
	}
}

func TestAuditConditionNegationAndDates(t *testing.T) {
	args := []any{}
	sql := buildAuditPredicate(parseFilter(t, `{"type":"group","combinator":"or","rules":[
		{"type":"rule","field":"createdAt","operator":"from","value":"2026-11-01T00:00:00Z"},
		{"type":"rule","field":"status","operator":"is","value":"failed","negated":true}]}`), &args)
	if !strings.Contains(sql, "e.created_at >= $1") || !strings.Contains(sql, "NOT lower(") {
		t.Fatalf("sql = %s", sql)
	}
}

func TestAuditConditionRejectsUnsafeInput(t *testing.T) {
	deep := `{"type":"rule","field":"action","operator":"is","value":"x"}`
	for i := 0; i < 5; i++ {
		deep = `{"type":"group","combinator":"and","rules":[` + deep + `]}`
	}
	for name, raw := range map[string]string{
		"unknown field":    `{"type":"rule","field":"e.action; DROP TABLE x","operator":"is","value":"x"}`,
		"unknown operator": `{"type":"rule","field":"action","operator":"regex","value":"x"}`,
		"bad combinator":   `{"type":"group","combinator":"xor","rules":[]}`,
		"missing value":    `{"type":"rule","field":"action","operator":"contains"}`,
		"list for one":     `{"type":"rule","field":"action","operator":"is","value":["a"]}`,
		"date operator":    `{"type":"rule","field":"createdAt","operator":"contains","value":"x"}`,
		"bad date":         `{"type":"rule","field":"createdAt","operator":"from","value":"yesterday"}`,
		"too deep":         deep,
		"not json":         `{`,
		"huge value":       `{"type":"rule","field":"action","operator":"is","value":"` + strings.Repeat("a", 201) + `"}`,
	} {
		if _, err := ParseAuditCondition(raw); err == nil {
			t.Errorf("%s: filter was accepted", name)
		}
	}
	if condition, err := ParseAuditCondition(""); condition != nil || err != nil {
		t.Fatalf("empty filter = %v, %v", condition, err)
	}
}

// Read-only; runs only against a database the caller points at.
func TestAuditConditionQueryRunsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to run the audit filter against PostgreSQL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db}
	for _, raw := range []string{
		`{"type":"group","combinator":"and","rules":[{"type":"rule","field":"action","operator":"contains","value":"plan"},{"type":"rule","field":"cluster","operator":"is","value":"m025-1"}]}`,
		`{"type":"group","combinator":"or","rules":[{"type":"rule","field":"actorKind","operator":"is","value":"people"},{"type":"rule","field":"status","operator":"is_any_of","value":["failed","queued"]},{"type":"rule","field":"namespace","operator":"empty"}]}`,
		`{"type":"group","combinator":"and","rules":[{"type":"rule","field":"createdAt","operator":"from","value":"2020-01-01T00:00:00Z"},{"type":"rule","field":"workspace","operator":"is_none_of","value":["nope"]},{"type":"rule","field":"resourceName","operator":"not_empty"}]}`,
	} {
		if _, err := store.QueryAuditEvents(context.Background(), AuditFilter{Condition: parseFilter(t, raw)}, 5); err != nil {
			t.Fatalf("query failed for %s: %v", raw, err)
		}
	}
}

// A role's category limit must hold in SQL, for the list and for a single event.
func TestAuditAllowedGroupsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to run the audit access policy against PostgreSQL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db}
	deployments := regexp.MustCompile(`^(sync|rollback|auto_sync|plan|deployment)\.|(decommission|rollback|resource_adopted)`)
	access := regexp.MustCompile(`^(user|workspace_member|oidc|oidc_provider|credential|session|mfa)\.|approval_policy`)
	ctx := context.Background()

	viewer, err := store.QueryAuditEvents(ctx, AuditFilter{AllowedGroups: []string{"deployments"}}, 300)
	if err != nil || len(viewer) == 0 {
		t.Fatalf("viewer events = %d, err = %v", len(viewer), err)
	}
	for _, e := range viewer {
		if !deployments.MatchString(e.Action) {
			t.Fatalf("a deployments-only role was shown %q", e.Action)
		}
	}
	deployer, err := store.QueryAuditEvents(ctx, AuditFilter{AllowedGroups: []string{"deployments", "changes"}}, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range deployer {
		if access.MatchString(e.Action) {
			t.Fatalf("a deployer was shown the access event %q", e.Action)
		}
	}
	// A hidden event must read as missing, even when asked for by ID.
	hidden, err := store.QueryAuditEvents(ctx, AuditFilter{Group: "access"}, 1)
	if err != nil || len(hidden) == 0 {
		t.Skip("no access events in this database to test against")
	}
	if _, err := store.AuditEventByID(ctx, hidden[0].ID, []string{"deployments"}); err == nil {
		t.Fatal("an access event was readable by a deployments-only role")
	}
	if _, err := store.AuditEventByID(ctx, hidden[0].ID, nil); err != nil {
		t.Fatalf("an unrestricted read must still work: %v", err)
	}
}
