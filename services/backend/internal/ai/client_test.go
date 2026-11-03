package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyzeLiteLLMContractAndTrustedSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer virtual-key" {
			t.Errorf("wrong LiteLLM request: %s", r.URL.Path)
		}
		var request struct {
			Model     string    `json:"model"`
			Messages  []Message `json:"messages"`
			MaxTokens int       `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "my-model" || request.MaxTokens != 1024 || len(request.Messages) != 2 || request.Messages[0].Role != "system" {
			t.Errorf("invalid request: %+v", request)
		}
		content := `{"summary":"Image changed","findings":[{"title":"Image update","detail":"Image changed in this plan","evidenceIds":["plan:1"]}],"sources":[{"id":"evil","url":"javascript:alert(1)"}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 42}})
	}))
	defer server.Close()
	for _, base := range []string{server.URL, server.URL + "/v1/"} {
		result, err := Analyze(context.Background(), Settings{BaseURL: base, Model: "my-model", APIKey: "virtual-key", MaxTokens: 1024}, "Explain", []Evidence{{ID: "plan:1", Label: "Plan", URL: "/applications/a?tab=changes", Data: map[string]string{"revision": "abc"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Sources) != 1 || result.Sources[0].ID != "plan:1" || result.Sources[0].Data != nil || result.Model != "my-model" || result.GeneratedAt.IsZero() {
			t.Fatalf("untrusted source or missing metadata: %+v", result)
		}
	}
}
func TestAnalyzeRejectsUnsupportedOrUncitedOutputs(t *testing.T) {
	for _, content := range []string{`{"summary":"x","findings":[{"title":"x","evidenceIds":["unknown"]}]}`, `{"summary":"x","findings":[{"title":"uncited"}]}`, `not JSON`} {
		t.Run(content, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Content: content}}}})
			}))
			defer server.Close()
			_, err := Analyze(context.Background(), Settings{BaseURL: server.URL, Model: "model", MaxTokens: 1024}, "task", []Evidence{{ID: "known"}}, nil)
			if err == nil {
				t.Fatal("invalid output accepted")
			}
		})
	}
}
func TestAnalyzeDoesNotFollowRedirectsOrExposeUpstreamSecrets(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusUnauthorized} {
		called := false
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		defer target.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			w.Write([]byte("super-secret-provider-error"))
		}))
		_, err := Analyze(context.Background(), Settings{BaseURL: server.URL, Model: "model", MaxTokens: 1024}, "task", nil, nil)
		server.Close()
		if err == nil || called || strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("unsafe upstream handling: called=%v err=%v", called, err)
		}
	}
}
func TestValidateRejectsCredentialURLs(t *testing.T) {
	for _, base := range []string{"file:///tmp/a", "https://user:password@host/v1", "https://host/v1?token=a", "https://host/v1#fragment"} {
		if Validate(Settings{BaseURL: base, Model: "model", MaxTokens: 1024}) == nil {
			t.Errorf("accepted %s", base)
		}
	}
}
