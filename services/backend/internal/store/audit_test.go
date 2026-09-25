package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeAuditDetailsRedactsHistoricalRollbackSettings(t *testing.T) {
	raw := json.RawMessage(`{"operationId":"op-1","rollbackTarget":{"kind":"snapshot","id":"snap-1","settings":{"helmValuesYaml":"password: secret"}}}`)
	safe := safeAuditDetails(raw)
	if strings.Contains(string(safe), "secret") || strings.Contains(string(safe), "settings") {
		t.Fatalf("rollback configuration was returned: %s", safe)
	}
	var details struct {
		OperationID    string `json:"operationId"`
		RollbackTarget struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"rollbackTarget"`
	}
	if err := json.Unmarshal(safe, &details); err != nil {
		t.Fatal(err)
	}
	if details.OperationID != "op-1" || details.RollbackTarget.Kind != "snapshot" || details.RollbackTarget.ID != "snap-1" {
		t.Fatalf("safe rollback provenance was lost: %s", safe)
	}
}
