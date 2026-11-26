package justscan

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (transport testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func TestPipelineScan(t *testing.T) {
	const org = "c7a11e8d-82a2-43fc-a978-a0319b1c7130"
	const id = "ce126332-1714-4c4c-9834-d3a423cd12ef"
	const path = "/justscan/api/v1/orgs/" + org + "/pipeline-scans"
	target := Target{Image: "registry.example.com/team/app:release", ExternalRef: "plan-1"}
	client, err := NewClient("https://scan.example.com/justscan", org, "token")
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing pipeline token")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == path:
			var request struct {
				Target
				Source string `json:"source"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Target != target || request.Source != "generic" {
				t.Errorf("unexpected scan request: %+v (%v)", request, err)
			}
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"scan_id":"`+id+`","status":"accepted","scan_status":"pending"}`)
		case r.Method == http.MethodGet && r.URL.Path == path+"/"+id:
			io.WriteString(w, `{"scan_id":"`+id+`","status":"completed","verdict":"fail","critical_count":2}`)
		default:
			http.NotFound(w, r)
		}
		return w.Result(), nil
	})
	accepted, err := client.Submit(t.Context(), target)
	if err != nil || accepted.ID != id || accepted.Status != "pending" {
		t.Fatalf("submit: %+v (%v)", accepted, err)
	}
	result, err := client.Status(t.Context(), accepted.ID)
	if err != nil || result.Verdict != "fail" || result.CriticalCount != 2 {
		t.Fatalf("status: %+v (%v)", result, err)
	}
}
