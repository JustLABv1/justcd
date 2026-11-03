package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/ai"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type planRisk struct {
	Level    string `json:"level"` // high | medium | low
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Resource string `json:"resource,omitempty"`
}

type planRiskSummary struct {
	Level  string     `json:"level"`
	Risks  []planRisk `json:"risks"`
	Create int        `json:"create"`
	Update int        `json:"update"`
	Delete int        `json:"delete"`
	Images []string   `json:"images,omitempty"`
}

var levelRank = map[string]int{"high": 0, "medium": 1, "low": 2}

func dig(value any, path ...string) any {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func parseObject(raw json.RawMessage) map[string]any {
	var object map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return object
}

// imagesOf returns container name -> image for any pod template-bearing manifest.
func imagesOf(object map[string]any) map[string]string {
	images := map[string]string{}
	var walk func(value any, depth int)
	walk = func(value any, depth int) {
		if depth > 8 {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "containers" || key == "initContainers" {
					if list, ok := item.([]any); ok {
						for _, entry := range list {
							name, _ := dig(entry, "name").(string)
							image, _ := dig(entry, "image").(string)
							if image != "" {
								images[name] = image
							}
						}
					}
					continue
				}
				if key == "spec" || key == "template" || key == "jobTemplate" {
					walk(item, depth+1)
				}
			}
		}
	}
	walk(object, 0)
	return images
}

func number(value any) (float64, bool) {
	n, ok := value.(float64)
	return n, ok
}

func imageTagIsFloating(image string) bool {
	if strings.Contains(image, "@sha256:") {
		return false
	}
	name := image[strings.LastIndex(image, "/")+1:]
	return !strings.Contains(name, ":") || strings.HasSuffix(name, ":latest")
}

// planRisks derives objective review points from a plan. It runs without AI and never calls Kubernetes.
func planRisks(plan core.Plan) planRiskSummary {
	summary := planRiskSummary{Risks: []planRisk{}}
	add := func(level, title, detail string, id core.Identity) {
		resource := id.Kind + " " + id.Name
		if id.Namespace != "" {
			resource = id.Kind + " " + id.Namespace + "/" + id.Name
		}
		summary.Risks = append(summary.Risks, planRisk{Level: level, Title: title, Detail: detail, Resource: resource})
	}
	imageChanges := map[string]bool{}
	for _, change := range plan.Changes {
		switch change.Kind {
		case "create":
			summary.Create++
		case "update":
			summary.Update++
		case "delete":
			summary.Delete++
		}
		id := change.Identity
		kind := id.Kind
		before, after := parseObject(change.Before), parseObject(change.After)
		switch {
		case change.Kind == "delete" && (kind == "PersistentVolumeClaim" || kind == "PersistentVolume"):
			add("high", "Deletes persisted storage", "Data on this volume can be lost permanently.", id)
		case change.Kind == "delete" && kind == "Namespace":
			add("high", "Deletes a namespace", "Everything in the namespace is removed with it.", id)
		case change.Kind == "delete" && (kind == "CustomResourceDefinition" || strings.HasSuffix(kind, "Definition")):
			add("high", "Deletes a custom resource definition", "All resources of this type are deleted with it.", id)
		case change.Kind == "delete":
			add("medium", "Deletes a resource", "The resource is removed from the cluster when this plan is applied.", id)
		}
		if change.RecreationRequired {
			add("high", "Recreates a StatefulSet", "The controller is deleted and recreated, which can interrupt service.", id)
		}
		if id.ClusterScoped && change.Kind != "delete" {
			add("medium", "Changes a cluster-wide resource", "This affects the whole cluster, not just this application's namespace.", id)
		}
		if change.Takeover {
			add("medium", "Takes over field ownership", "Another controller may currently manage these fields and could fight this change.", id)
		}
		switch kind {
		case "Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding":
			if change.Kind != "delete" {
				add("high", "Changes permissions", "RBAC rules are created or modified; review who gains access.", id)
			}
		case "NetworkPolicy":
			if change.Kind != "delete" {
				add("medium", "Changes network policy", "Traffic that is allowed or blocked between workloads may change.", id)
			}
		case "Secret":
			if change.Kind == "update" {
				add("low", "Secret values change", "Values are hidden in the plan; confirm the new secret is intended.", id)
			}
		case "Service":
			if change.Kind == "update" {
				for _, path := range change.ChangedPaths {
					if strings.HasPrefix(path, "/spec/selector") {
						add("high", "Changes a Service selector", "Traffic may stop reaching the intended pods.", id)
						break
					}
				}
				if t := dig(after, "spec", "type"); t != dig(before, "spec", "type") && (t == "LoadBalancer" || t == "NodePort") {
					add("medium", "Exposes a Service externally", fmt.Sprintf("Service type becomes %v.", t), id)
				}
			}
		case "Ingress":
			if change.Kind == "update" {
				add("medium", "Changes an Ingress", "Public routes or hostnames may change.", id)
			}
		case "Deployment", "StatefulSet":
			b, bok := number(dig(before, "spec", "replicas"))
			a, aok := number(dig(after, "spec", "replicas"))
			if change.Kind == "update" && bok && aok && a < b {
				if a == 0 {
					add("high", "Scales a workload to zero", fmt.Sprintf("Replicas go from %d to 0; the workload stops serving.", int(b)), id)
				} else {
					add("medium", "Reduces replicas", fmt.Sprintf("Replicas go from %d to %d.", int(b), int(a)), id)
				}
			}
		}
		if before != nil || after != nil {
			beforeImages, afterImages := imagesOf(before), imagesOf(after)
			for name, image := range afterImages {
				if old := beforeImages[name]; old != image {
					if old != "" {
						imageChanges[fmt.Sprintf("%s/%s: %s → %s", id.Name, name, old, image)] = true
					} else {
						imageChanges[fmt.Sprintf("%s/%s: %s", id.Name, name, image)] = true
					}
					if imageTagIsFloating(image) {
						add("medium", "Uses a floating image tag", fmt.Sprintf("%s has no fixed version (%s); a rollout can pull different content later.", name, image), id)
					}
				}
			}
			if change.Kind == "update" {
				if reduced := reducedLimits(before, after); reduced != "" {
					add("medium", "Lowers resource limits", reduced, id)
				}
			}
		}
	}
	for text := range imageChanges {
		summary.Images = append(summary.Images, text)
	}
	sort.Strings(summary.Images)
	sort.SliceStable(summary.Risks, func(i, j int) bool { return levelRank[summary.Risks[i].Level] < levelRank[summary.Risks[j].Level] })
	summary.Level = "low"
	if len(summary.Risks) > 0 {
		summary.Level = summary.Risks[0].Level
	}
	return summary
}

func reducedLimits(before, after map[string]any) string {
	collect := func(object map[string]any) map[string]map[string]string {
		out := map[string]map[string]string{}
		var walk func(value any, depth int)
		walk = func(value any, depth int) {
			if depth > 8 {
				return
			}
			if m, ok := value.(map[string]any); ok {
				for key, item := range m {
					if key == "containers" {
						if list, ok := item.([]any); ok {
							for _, entry := range list {
								name, _ := dig(entry, "name").(string)
								limits := map[string]string{}
								if l, ok := dig(entry, "resources", "limits").(map[string]any); ok {
									for resource, v := range l {
										limits[resource] = fmt.Sprint(v)
									}
								}
								out[name] = limits
							}
						}
						continue
					}
					if key == "spec" || key == "template" || key == "jobTemplate" {
						walk(item, depth+1)
					}
				}
			}
		}
		walk(object, 0)
		return out
	}
	old, next := collect(before), collect(after)
	for name, limits := range old {
		for resource, previous := range limits {
			if current, ok := next[name][resource]; ok && quantityValue(current) < quantityValue(previous) {
				return fmt.Sprintf("%s %s limit goes from %s to %s.", name, resource, previous, current)
			}
		}
	}
	return ""
}

// quantityValue parses common Kubernetes quantities well enough to compare two values of the same resource.
func quantityValue(value string) float64 {
	suffixes := []struct {
		text  string
		scale float64
	}{{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"m", 0.001}, {"k", 1e3}, {"M", 1e6}, {"G", 1e9}}
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix.text) {
			var n float64
			if _, err := fmt.Sscanf(strings.TrimSuffix(value, suffix.text), "%g", &n); err == nil {
				return n * suffix.scale
			}
		}
	}
	var n float64
	_, _ = fmt.Sscanf(value, "%g", &n)
	return n
}

type planReviewResponse struct {
	PlanID      string         `json:"planId"`
	Level       string         `json:"level"`
	Risks       []planRisk     `json:"risks"`
	Counts      map[string]int `json:"counts"`
	Images      []string       `json:"images"`
	Source      string         `json:"source"` // ai | rules
	Stale       bool           `json:"stale"`
	Available   bool           `json:"available"`
	Summary     string         `json:"summary,omitempty"`
	Concerns    []planConcern  `json:"concerns"`
	Checks      []string       `json:"checks"`
	Limitations []string       `json:"limitations"`
	Model       string         `json:"model,omitempty"`
	GeneratedAt *time.Time     `json:"generatedAt,omitempty"`
}

type planConcern struct {
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type storedPlanReview struct {
	Summary     string        `json:"summary"`
	Concerns    []planConcern `json:"concerns"`
	Checks      []string      `json:"checks"`
	Limitations []string      `json:"limitations"`
}

func (s *Server) planReviewScope(w http.ResponseWriter, r *http.Request) (store.PlanRecord, store.Application, bool) {
	workspace := r.PathValue("workspaceID")
	record, err := s.Store.PlanByID(r.Context(), r.PathValue("planID"))
	if err != nil {
		writeError(w, 404, "plan not found")
		return record, store.Application{}, false
	}
	app, err := s.Store.ApplicationByID(r.Context(), record.Plan.ApplicationID)
	if err != nil || app.WorkspaceID != workspace {
		writeError(w, 404, "plan not found in this workspace")
		return record, app, false
	}
	return record, app, true
}

func (s *Server) buildPlanReview(ctx context.Context, record store.PlanRecord, available bool) (planReviewResponse, error) {
	risks := planRisks(record.Plan)
	response := planReviewResponse{PlanID: record.ID, Level: risks.Level, Risks: risks.Risks, Images: risks.Images, Available: available, Source: "rules",
		Counts: map[string]int{"create": risks.Create, "update": risks.Update, "delete": risks.Delete}, Concerns: []planConcern{}, Checks: []string{}, Limitations: []string{}}
	if response.Images == nil {
		response.Images = []string{}
	}
	cached, found, err := s.Store.AIPlanReview(ctx, record.ID)
	if err != nil {
		return response, err
	}
	var stored storedPlanReview
	if found && json.Unmarshal(cached.Result, &stored) == nil {
		generated := cached.GeneratedAt
		response.Source, response.Summary, response.Model, response.GeneratedAt = "ai", stored.Summary, cached.Model, &generated
		response.Stale = cached.PlanDigest != record.Plan.Digest
		if stored.Concerns != nil {
			response.Concerns = stored.Concerns
		}
		if stored.Checks != nil {
			response.Checks = stored.Checks
		}
		if stored.Limitations != nil {
			response.Limitations = stored.Limitations
		}
	}
	return response, nil
}

func (s *Server) getPlanReview(w http.ResponseWriter, r *http.Request) {
	if !s.requireWorkspaceRole(w, r, r.PathValue("workspaceID"), "viewer") {
		return
	}
	record, _, ok := s.planReviewScope(w, r)
	if !ok {
		return
	}
	response, err := s.buildPlanReview(r.Context(), record, s.aiAvailable(r, r.PathValue("workspaceID")))
	if err != nil {
		writeStoreError(w, "could not load plan review")
		return
	}
	writeJSON(w, 200, response)
}

func (s *Server) postPlanReview(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	settings, release, ok := s.aiGate(w, r, workspace)
	if !ok {
		return
	}
	defer release()
	record, app, ok := s.planReviewScope(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	if err := s.reviewPlan(ctx, settings, app, record, currentUser(r).ID, "manual"); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	response, err := s.buildPlanReview(r.Context(), record, true)
	if err != nil {
		writeStoreError(w, "could not load plan review")
		return
	}
	writeJSON(w, 200, response)
}

const planReviewTask = `Brief a human who must approve this deployment plan. Evidence "plan:<id>" gives the plan counts and the changed resource list; "risks:<id>" lists review points derived by deterministic rules, which are facts; "changes:<id>" lists changed paths per resource and image changes. Return "summary" (two sentences: what this plan does in plain language), "findings" (additional concerns that are NOT already covered by the rules' risks; at most 5; each with "title", "detail", "severity" high|medium|low and evidenceIds; return an empty list if there are none), and "nextSteps" (up to 4 specific things to verify before approving, phrased as short imperative sentences). Do not restate the rule risks as findings. Do not claim the plan is safe; say what you checked and what the evidence cannot show.`

func (s *Server) reviewPlan(ctx context.Context, settings ai.Settings, app store.Application, record store.PlanRecord, actor, trigger string) error {
	risks := planRisks(record.Plan)
	changes := []any{}
	for _, change := range record.Plan.Changes {
		if len(changes) >= 60 {
			break
		}
		changes = append(changes, map[string]any{"kind": change.Kind, "identity": change.Identity, "changedPaths": change.ChangedPaths, "takeover": change.Takeover, "recreationRequired": change.RecreationRequired})
	}
	base := "/applications/" + app.ID
	evidence := []ai.Evidence{
		{ID: "plan:" + record.ID, Label: "Plan " + record.ID, URL: base + "?tab=changes&planId=" + record.ID, Data: aiScrub(map[string]any{"application": app.Name, "revision": record.Plan.Revision, "requiresApproval": record.Plan.RequiresApproval, "create": risks.Create, "update": risks.Update, "delete": risks.Delete, "expiresAt": record.ExpiresAt})},
		{ID: "risks:" + record.ID, Label: "Rule-based review points", URL: base + "?tab=changes&planId=" + record.ID, Data: aiScrub(risks)},
		{ID: "changes:" + record.ID, Label: "Changed resources", URL: base + "?tab=changes&planId=" + record.ID, Data: aiScrub(map[string]any{"changes": changes, "images": risks.Images, "totalChanges": len(record.Plan.Changes)})},
	}
	result, err := ai.Analyze(ctx, settings, planReviewTask, evidence, nil)
	if err != nil {
		return err
	}
	redactResult(&result)
	stored := storedPlanReview{Summary: clip(result.Summary, 500), Concerns: []planConcern{}, Checks: []string{}, Limitations: []string{}}
	for _, finding := range result.Findings {
		level := finding.Severity
		if _, valid := levelRank[level]; !valid {
			level = "medium"
		}
		if len(stored.Concerns) < 5 {
			stored.Concerns = append(stored.Concerns, planConcern{Level: level, Title: clip(finding.Title, 100), Detail: clip(finding.Detail, 400)})
		}
	}
	for _, step := range result.NextSteps {
		if len(stored.Checks) < 4 {
			stored.Checks = append(stored.Checks, clip(step, 200))
		}
	}
	stored.Limitations = append(stored.Limitations, "Review covers the plan as stored; live state may have changed since it was built.")
	for _, limitation := range result.Limitations {
		if len(stored.Limitations) < 4 {
			stored.Limitations = append(stored.Limitations, clip(limitation, 300))
		}
	}
	raw, _ := json.Marshal(stored)
	if err := s.Store.Audit(ctx, actor, "ai.plan_review_generated", "plan", record.ID, map[string]any{"trigger": trigger, "model": settings.Model}); err != nil {
		return fmt.Errorf("could not audit AI plan review")
	}
	return s.Store.SaveAIPlanReview(ctx, record.ID, store.AIPlanReviewRecord{PlanDigest: record.Plan.Digest, Model: settings.Model, Result: raw, GeneratedAt: time.Now().UTC()})
}

// autoReviewPlans briefs approvers for fresh plans that need approval, a couple per tick.
func (s *Server) autoReviewPlans(ctx context.Context, workspaceID string) {
	apps, err := s.Store.ListApplications(ctx, workspaceID)
	if err != nil {
		return
	}
	settings, err := s.loadAI(ctx)
	if err != nil || !settings.Enabled {
		return
	}
	done := 0
	for _, app := range apps {
		if done >= 2 || ctx.Err() != nil {
			return
		}
		plans, err := s.Store.ListPlans(ctx, app.ID, 1)
		if err != nil || len(plans) == 0 {
			continue
		}
		record := plans[0]
		if record.Status != "current" || !record.Plan.RequiresApproval || !record.ExpiresAt.After(time.Now()) || time.Since(record.CreatedAt) > 24*time.Hour {
			continue
		}
		if cached, found, e := s.Store.AIPlanReview(ctx, record.ID); e != nil || (found && cached.PlanDigest == record.Plan.Digest) {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
		_ = s.reviewPlan(runCtx, settings, app, record, "", "automatic")
		cancel()
		done++
	}
}
