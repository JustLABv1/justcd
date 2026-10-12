package store

import (
	"encoding/json"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

type ResourceProgress struct {
	Identity     core.Identity `json:"identity"`
	Action       string        `json:"action"`
	Status       string        `json:"status"`
	Stage        string        `json:"stage,omitempty"`
	AttemptCount int           `json:"attemptCount"`
	StartedAt    *time.Time    `json:"startedAt,omitempty"`
	FinishedAt   *time.Time    `json:"finishedAt,omitempty"`
	Message      string        `json:"message,omitempty"`
}

func NewOperationProgress(plan core.Plan) OperationProgress {
	p := OperationProgress{Phase: "queued", Completed: []core.Identity{}, Resources: []ResourceProgress{}}
	seen := map[string]bool{}
	for _, identity := range plan.NamespaceCreations {
		seen[identity.Key()] = true
		p.Resources = append(p.Resources, ResourceProgress{Identity: identity, Action: "create_namespace", Status: "queued"})
	}
	for _, change := range plan.Changes {
		if !seen[change.Identity.Key()] {
			p.Resources = append(p.Resources, ResourceProgress{Identity: change.Identity, Action: string(change.Kind), Status: "queued"})
			seen[change.Identity.Key()] = true
		}
	}
	p.Total = len(p.Resources)
	return p
}

func (p *OperationProgress) BeginResource(identity core.Identity, attempt int) {
	now := time.Now().UTC()
	p.Current = &identity
	for i := range p.Resources {
		r := &p.Resources[i]
		if r.Identity.Key() == identity.Key() {
			r.Status, r.Stage, r.AttemptCount, r.StartedAt = "running", p.Phase, attempt, &now
			if attempt > 1 {
				r.Status = "retrying"
			}
		}
	}
}

func (p *OperationProgress) CompleteResource(identity core.Identity) {
	now := time.Now().UTC()
	p.Completed = append(p.Completed, identity)
	p.Current = nil
	for i := range p.Resources {
		r := &p.Resources[i]
		if r.Identity.Key() == identity.Key() {
			r.Status, r.FinishedAt = "succeeded", &now
		}
	}
}

// Stop preserves confirmed mutations. An in-flight step may have changed the
// cluster even when the client never received its response; never call it skipped.
func (p *OperationProgress) Stop(message string) {
	now := time.Now().UTC()
	for i := range p.Resources {
		r := &p.Resources[i]
		switch r.Status {
		case "queued":
			r.Status, r.Message = "skipped", "Not attempted"
		case "running", "retrying":
			r.Status, r.Message = "failed", message
		default:
			continue
		}
		r.FinishedAt = &now
	}
	p.Current = nil
}

// Historical operations can contain {} or completed:null. Always return arrays.
func (p OperationProgress) MarshalJSON() ([]byte, error) {
	type progress OperationProgress
	if p.Completed == nil {
		p.Completed = []core.Identity{}
	}
	if p.Resources == nil {
		p.Resources = []ResourceProgress{}
	}
	return json.Marshal(progress(p))
}

// Normalize terminal results from older executors and crash recovery on reads.
func (o *Operation) NormalizeProgress() {
	if o.Status == "failed" || o.Status == "cancelled" {
		o.Progress.Stop(o.Message)
		for i := range o.Progress.Resources {
			if o.Progress.Resources[i].Status == "skipped" || o.Progress.Resources[i].Status == "failed" {
				o.Progress.Resources[i].FinishedAt = o.FinishedAt
			}
		}
	}
}
