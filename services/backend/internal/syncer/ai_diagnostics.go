package syncer

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type AIDiagnostic struct {
	Identity    core.Identity `json:"identity"`
	Events      []any         `json:"events,omitempty"`
	Logs        []any         `json:"logs,omitempty"`
	Warning     string        `json:"warning,omitempty"`
	CollectedAt time.Time     `json:"collectedAt"`
}

// AIDiagnostics reads only resources already observed as belonging to this app.
// Logs require a separate user opt-in because arbitrary application output can contain secrets.
func (s *Service) AIDiagnostics(ctx context.Context, app store.Application, includeLogs bool) ([]AIDiagnostic, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	observed, err := s.Store.ObservedResources(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	// Prioritize unhealthy pods so the optional log budget reaches the most useful evidence.
	sort.SliceStable(observed, func(i, j int) bool {
		rank := func(item store.ObservedResource) int {
			if item.Identity.Kind == "Pod" {
				if item.HealthSummary.WaitingReason != "" || item.HealthSummary.FailureReason != "" {
					return 0
				}
				return 1
			}
			return 2
		}
		return rank(observed[i]) < rank(observed[j])
	})
	cluster, err := s.Store.ClusterByID(ctx, app.ClusterID)
	if err != nil {
		return nil, err
	}
	fallback, err := s.Store.WorkspaceClusterCredential(ctx, app.WorkspaceID, app.ClusterID)
	if err != nil {
		return nil, err
	}
	out := []AIDiagnostic{}
	logsRead := 0
	for _, binding := range app.Namespaces {
		if ctx.Err() != nil {
			break
		}
		stored, e := s.Store.NamespaceBinding(ctx, app.WorkspaceID, app.ClusterID, binding.Namespace)
		if e != nil {
			return nil, e
		}
		credential := stored.CredentialID
		if credential == nil {
			credential = fallback
		}
		client, e := kube.ForWorkspaceBinding(ctx, s.Store, s.EncryptionKey, cluster, credential, false, app.WorkspaceID, binding.Namespace)
		if e != nil {
			out = append(out, AIDiagnostic{Warning: "Live diagnostics unavailable for namespace " + binding.Namespace, CollectedAt: time.Now().UTC()})
			continue
		}
		for _, item := range observed {
			if len(out) >= 10 || ctx.Err() != nil {
				break
			}
			if item.Source != "kubernetes" || item.UID == "" || item.Identity.Namespace != binding.Namespace {
				continue
			}
			sample := AIDiagnostic{Identity: item.Identity, CollectedAt: time.Now().UTC()}
			readCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			events, e := client.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "events"}).Namespace(binding.Namespace).List(readCtx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + item.UID, Limit: 20})
			stop()
			if e != nil {
				sample.Warning = "Events unavailable; check read/list permissions or connection"
			} else {
				for index, event := range events.Items {
					if index >= 10 {
						break
					}
					sample.Events = append(sample.Events, map[string]any{"reason": event.Object["reason"], "message": event.Object["message"], "type": event.Object["type"], "count": event.Object["count"], "lastTimestamp": event.Object["lastTimestamp"]})
				}
			}
			if includeLogs && item.Identity.Kind == "Pod" && logsRead < 2 {
				if kube.AgentEnabled(ctx, s.Store, app.ClusterID) {
					sample.Warning += "; Pod logs are unavailable through the current cluster agent protocol"
				} else {
					logsRead++
					pod, e := client.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(binding.Namespace).Get(ctx, item.Identity.Name, metav1.GetOptions{})
					if e != nil || string(pod.GetUID()) != item.UID {
						sample.Warning += "; Pod changed or is unavailable; logs omitted"
					} else {
						containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
						for index, value := range containers {
							if index >= 2 {
								break
							}
							container, _ := value.(map[string]any)
							name, _ := container["name"].(string)
							logCtx, stop := context.WithTimeout(ctx, 3*time.Second)
							stream, e := client.Discovery.RESTClient().Get().AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", binding.Namespace, item.Identity.Name)).Param("container", name).Param("tailLines", "50").Param("limitBytes", "4096").Param("timestamps", "true").Stream(logCtx)
							if e != nil {
								sample.Warning += "; Container logs unavailable"
							} else {
								raw, readErr := io.ReadAll(io.LimitReader(stream, 4096))
								stream.Close()
								if readErr == nil {
									sample.Logs = append(sample.Logs, map[string]any{"container": name, "tail": string(raw), "maxBytes": 4096})
								}
							}
							stop()
						}
					}
				}
			}
			out = append(out, sample)
		}
	}
	if ctx.Err() != nil {
		out = append(out, AIDiagnostic{Warning: "Live diagnostic time limit reached; evidence may be incomplete", CollectedAt: time.Now().UTC()})
	}
	return out, nil
}
