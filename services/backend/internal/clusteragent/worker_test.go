package clusteragent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
)

func TestRunTaskConcurrencyAndDelivery(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded=%t", bounded), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tasks := make(chan agentprotocol.Request, 12)
			for i := range 12 {
				tasks <- agentprotocol.Request{ID: fmt.Sprint(i), Lease: "lease", Profile: "default", WorkspaceID: "w", Namespace: "dev", Method: "GET", URI: fmt.Sprintf("/api/v1/namespaces/dev/configmaps/task-%d", i), Deadline: time.Now().Add(5 * time.Second)}
			}
			started := make(chan struct{}, 12)
			fastDelivered := make(chan struct{}, 1)
			var fastExecutions, deliveries atomic.Int32
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				started <- struct{}{}
				if bounded || strings.HasSuffix(r.URL.Path, "task-0") {
					<-ctx.Done()
					return
				}
				if strings.HasSuffix(r.URL.Path, "task-1") {
					fastExecutions.Add(1)
				}
				_, _ = w.Write([]byte(`{"kind":"ConfigMap"}`))
			}))
			defer local.Close()
			central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/agents/heartbeat":
					w.WriteHeader(204)
				case r.URL.Path == "/api/v1/agents/tasks":
					select {
					case task := <-tasks:
						_ = json.NewEncoder(w).Encode(task)
					case <-ctx.Done():
						w.WriteHeader(204)
					}
				case r.URL.Path == "/api/v1/agents/tasks/1/result":
					if deliveries.Add(1) == 1 {
						w.WriteHeader(503) // Retry result delivery, never execution.
						return
					}
					fastDelivered <- struct{}{}
					w.WriteHeader(204)
				default:
					w.WriteHeader(204)
				}
			}))
			defer central.Close()
			a := &Agent{Config: Config{ClusterID: "physical", IdentityFile: t.TempDir() + "/identity", ServerURL: central.URL, Profiles: []agentprotocol.Profile{{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"dev"}}}}, HTTP: central.Client(), Local: map[string]*http.Client{"default": local.Client()}, KubernetesURL: local.URL}
			if err := a.saveIdentity(agentprotocol.Identity{Token: "test", ClusterUID: "physical", ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- a.Run(ctx) }()
			// Always cancel before waiting for httptest handlers to finish.
			defer cancel()
			if bounded {
				for range 8 {
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal("agent did not run eight requests concurrently")
					}
				}
				select {
				case <-started:
					t.Fatal("agent exceeded its concurrency bound")
				case <-time.After(100 * time.Millisecond):
				}
			} else {
				select {
				case <-fastDelivered:
					if fastExecutions.Load() != 1 || deliveries.Load() != 2 {
						t.Fatal("result delivery retry re-executed the Kubernetes request")
					}
				case <-ctx.Done():
					t.Fatal("slow Kubernetes request blocked another task or result delivery")
				}
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("agent workers did not stop on cancellation")
			}
		})
	}
}
