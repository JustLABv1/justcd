package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolDiscovery(t *testing.T) {
	for _, allow := range []bool{false, true} {
		s := &Server{}
		server := s.newMCPServer(store.MCPCredential{AllowPlan: allow}, store.User{})
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		for _, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`} {
			r := httptest.NewRequest("POST", "/api/v1/mcp", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("MCP-Protocol-Version", "2025-11-25")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			var response struct {
				Error  any `json:"error"`
				Result struct {
					Tools []struct {
						Name        string `json:"name"`
						Annotations struct {
							ReadOnly bool `json:"readOnlyHint"`
						} `json:"annotations"`
					} `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			if strings.Contains(body, "tools/list") {
				names := map[string]bool{}
				for _, tool := range response.Result.Tools {
					names[tool.Name] = true
					if tool.Name != "create_plan" && !tool.Annotations.ReadOnly {
						t.Errorf("%s not read only", tool.Name)
					}
				}
				if names["create_plan"] != allow {
					t.Fatalf("plan capability mismatch: %v", names)
				}
				for _, forbidden := range []string{"apply_plan", "approve_plan", "delete_application"} {
					if names[forbidden] {
						t.Fatal(forbidden)
					}
				}
				if !names["get_plan"] || !names["list_applications"] {
					t.Fatal(names)
				}
			}
		}
	}
}
func TestMCPRejectsBrowserAndMissingCredential(t *testing.T) {
	s := &Server{}
	for _, origin := range []string{"", "https://evil.invalid"} {
		r := httptest.NewRequest("POST", "https://justcd.invalid/api/v1/mcp", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.serveMCP(w, r)
		want := 401
		if origin != "" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("got %d want %d", w.Code, want)
		}
	}
}
func TestMCPApplicationOmitsConfiguration(t *testing.T) {
	raw, _ := json.Marshal(mcpApplication(store.Application{ID: "app", HelmValuesYAML: "password: sensitive", TargetHelmValuesYAML: "sensitive"}))
	if strings.Contains(string(raw), "sensitive") || strings.Contains(string(raw), "helmValues") {
		t.Fatal(string(raw))
	}
}

func TestMCPRedactsCredentialsAndEnvironment(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"bindings":[{"credentialRef":"hidden"}],"env":[{"name":"DB_PASSWORD","value":"hidden"},{"name":"REGION","value":"eu"}]}`), &value); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mcpRedact(value))
	if strings.Contains(string(raw), "hidden") || !strings.Contains(string(raw), "eu") {
		t.Fatal(string(raw))
	}
}
