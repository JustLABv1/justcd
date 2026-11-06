package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidOIDCClaim(t *testing.T) {
	long := `"` + strings.Repeat("a", 257) + `"`
	cases := []struct {
		name, raw string
		want      []string
		wantErr   bool
	}{
		{"missing", ``, nil, false},
		{"null", `null`, nil, false},
		{"array", `["a","b"]`, []string{"a", "b"}, false},
		{"single string", `"admins"`, []string{"admins"}, false},
		{"empty string", `""`, nil, true},
		{"object", `{"id":"x"}`, nil, true},
		{"mixed array", `["a",1]`, nil, true},
		{"empty entry", `["a",""]`, nil, true},
		{"too long", `[` + long + `]`, nil, true},
	}
	for _, c := range cases {
		got, err := validOIDCClaim(json.RawMessage(c.raw))
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
