package clusteragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
)

func pollServer(t *testing.T, respond func(http.ResponseWriter, *http.Request)) *Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/tasks" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		respond(w, r)
	}))
	t.Cleanup(server.Close)
	return &Agent{Config: Config{ServerURL: server.URL}, HTTP: server.Client()}
}

func TestPollTasksReadsBatches(t *testing.T) {
	a := pollServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("max") != "3" {
			t.Errorf("poll must ask for the free worker count, got %q", r.URL.Query().Get("max"))
		}
		_ = json.NewEncoder(w).Encode(agentprotocol.TaskBatch{Tasks: []agentprotocol.Request{{ID: "a"}, {ID: "b"}}})
	})
	tasks, status, err := a.pollTasks(context.Background(), 3)
	if err != nil || status != 200 || len(tasks) != 2 || tasks[0].ID != "a" || tasks[1].ID != "b" {
		t.Fatalf("got %+v %d %v", tasks, status, err)
	}
}

func TestPollTasksAcceptsSingleTaskFromOlderServers(t *testing.T) {
	a := pollServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(agentprotocol.Request{ID: "only", Method: "GET"})
	})
	tasks, _, err := a.pollTasks(context.Background(), 8)
	if err != nil || len(tasks) != 1 || tasks[0].ID != "only" || tasks[0].Method != "GET" {
		t.Fatalf("got %+v %v", tasks, err)
	}
}

func TestPollTasksHandlesIdleAndOversizedBatches(t *testing.T) {
	idle := pollServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	if tasks, status, err := idle.pollTasks(context.Background(), 2); err != nil || status != 204 || len(tasks) != 0 {
		t.Fatalf("idle poll: %+v %d %v", tasks, status, err)
	}
	greedy := pollServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(agentprotocol.TaskBatch{Tasks: []agentprotocol.Request{{ID: "a"}, {ID: "b"}}})
	})
	if tasks, _, err := greedy.pollTasks(context.Background(), 1); err == nil || len(tasks) != 0 {
		t.Fatalf("a batch larger than the free workers must be rejected: %+v %v", tasks, err)
	}
}
