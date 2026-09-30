package store

import (
	"encoding/json"
	"testing"
)

func TestOperationProgressSerializesMissingCompletedAsArray(t *testing.T) {
	for _, raw := range []string{`{}`, `{"completed":null}`, `{"completed":[]}`} {
		var p OperationProgress
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if string(result["completed"]) != "[]" {
			t.Fatalf("%s returned %s", raw, encoded)
		}
	}
}
