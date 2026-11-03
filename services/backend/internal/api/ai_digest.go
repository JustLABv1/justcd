package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/justlab/justcd/services/backend/internal/ai"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type digestApplication struct {
	ApplicationID string `json:"applicationId"`
	Name          string `json:"name"`
	Succeeded     int    `json:"succeeded"`
	Failed        int    `json:"failed"`
	Rollbacks     int    `json:"rollbacks"`
	LastMessage   string `json:"lastMessage,omitempty"`
}

type digestStats struct {
	Period       string              `json:"period"`
	Since        time.Time           `json:"since"`
	Until        time.Time           `json:"until"`
	Operations   int                 `json:"operations"`
	Succeeded    int                 `json:"succeeded"`
	Failed       int                 `json:"failed"`
	Rollbacks    int                 `json:"rollbacks"`
	Applications []digestApplication `json:"applications"`
	Attention    []triageItem        `json:"attention"`
	Waiting      []digestWaiting     `json:"waiting"`
}

type digestWaiting struct {
	ApplicationID string    `json:"applicationId"`
	Name          string    `json:"name"`
	PlanID        string    `json:"planId"`
	Changes       int       `json:"changes"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type digestItem struct {
	Section         string   `json:"section"` // deployed | failed | attention | waiting
	Title           string   `json:"title"`
	Detail          string   `json:"detail"`
	ApplicationID   string   `json:"applicationId,omitempty"`
	ApplicationName string   `json:"applicationName,omitempty"`
	EvidenceIDs     []string `json:"evidenceIds,omitempty"`
}

type storedDigest struct {
	Summary     string       `json:"summary"`
	Items       []digestItem `json:"items"`
	Limitations []string     `json:"limitations"`
	Stats       digestStats  `json:"stats"`
}

type digestResponse struct {
	Period      string       `json:"period"`
	Stats       digestStats  `json:"stats"`
	Source      string       `json:"source"` // ai | rules
	Stale       bool         `json:"stale"`
	Available   bool         `json:"available"`
	Summary     string       `json:"summary,omitempty"`
	Items       []digestItem `json:"items"`
	Limitations []string     `json:"limitations"`
	Model       string       `json:"model,omitempty"`
	GeneratedAt *time.Time   `json:"generatedAt,omitempty"`
}

func digestWindow(period string) (time.Duration, bool) {
	switch period {
	case "day":
		return 24 * time.Hour, true
	case "week":
		return 7 * 24 * time.Hour, true
	}
	return 0, false
}

// computeDigest gathers objective workspace activity for the period; it needs no AI.
func (s *Server) computeDigest(ctx context.Context, workspaceID, period string) (digestStats, []store.Application, error) {
	window, _ := digestWindow(period)
	now := time.Now().UTC()
	stats := digestStats{Period: period, Since: now.Add(-window), Until: now, Applications: []digestApplication{}, Attention: []triageItem{}, Waiting: []digestWaiting{}}
	apps, err := s.Store.ListApplications(ctx, workspaceID)
	if err != nil {
		return stats, nil, err
	}
	for _, app := range apps {
		operations, err := s.Store.ListOperations(ctx, app.ID, 30)
		if err != nil {
			return stats, nil, err
		}
		entry := digestApplication{ApplicationID: app.ID, Name: app.Name}
		for _, operation := range operations {
			if operation.StartedAt.Before(stats.Since) {
				continue
			}
			stats.Operations++
			if operation.Type == "rollback" {
				entry.Rollbacks++
				stats.Rollbacks++
			}
			switch operation.Status {
			case "succeeded":
				entry.Succeeded++
				stats.Succeeded++
			case "failed":
				entry.Failed++
				stats.Failed++
				if entry.LastMessage == "" {
					entry.LastMessage = clip(operation.Message, 200)
				}
			}
		}
		if entry.Succeeded+entry.Failed+entry.Rollbacks > 0 {
			stats.Applications = append(stats.Applications, entry)
		}
		if plans, e := s.Store.ListPlans(ctx, app.ID, 1); e == nil && len(plans) > 0 {
			plan := plans[0]
			if plan.Status == "current" && plan.Plan.RequiresApproval && plan.ExpiresAt.After(now) && len(stats.Waiting) < 20 {
				stats.Waiting = append(stats.Waiting, digestWaiting{ApplicationID: app.ID, Name: app.Name, PlanID: plan.ID, Changes: len(plan.Plan.Changes), ExpiresAt: plan.ExpiresAt})
			}
		}
	}
	sort.SliceStable(stats.Applications, func(i, j int) bool {
		if stats.Applications[i].Failed != stats.Applications[j].Failed {
			return stats.Applications[i].Failed > stats.Applications[j].Failed
		}
		return stats.Applications[i].Name < stats.Applications[j].Name
	})
	if len(stats.Applications) > 30 {
		stats.Applications = stats.Applications[:30]
	}
	if signals, _, _, err := s.triageInputs(ctx, workspaceID); err == nil {
		items := ruleItems(signals)
		if len(items) > 8 {
			items = items[:8]
		}
		stats.Attention = items
	}
	return stats, apps, nil
}

func (s *Server) buildDigest(ctx context.Context, workspaceID, period string, available bool) (digestResponse, error) {
	stats, _, err := s.computeDigest(ctx, workspaceID, period)
	if err != nil {
		return digestResponse{}, err
	}
	response := digestResponse{Period: period, Stats: stats, Source: "rules", Available: available, Items: []digestItem{}, Limitations: []string{}}
	record, found, err := s.Store.AIDigest(ctx, workspaceID, period)
	if err != nil {
		return response, err
	}
	var stored storedDigest
	if found && json.Unmarshal(record.Result, &stored) == nil {
		generated := record.GeneratedAt
		window, _ := digestWindow(period)
		response.Source, response.Summary, response.Model, response.GeneratedAt = "ai", stored.Summary, record.Model, &generated
		response.Stale = time.Since(generated) > window/4
		if stored.Items != nil {
			response.Items = stored.Items
		}
		if stored.Limitations != nil {
			response.Limitations = stored.Limitations
		}
	}
	return response, nil
}

func validPeriod(w http.ResponseWriter, r *http.Request) (string, bool) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "day"
	}
	if _, ok := digestWindow(period); !ok {
		writeError(w, 400, "period must be day or week")
		return "", false
	}
	return period, true
}

func (s *Server) getDigest(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	period, ok := validPeriod(w, r)
	if !ok {
		return
	}
	response, err := s.buildDigest(r.Context(), workspaceID, period, s.aiAvailable(r, workspaceID))
	if err != nil {
		writeStoreError(w, "could not load digest")
		return
	}
	writeJSON(w, 200, response)
}

func (s *Server) postDigest(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	period, ok := validPeriod(w, r)
	if !ok {
		return
	}
	settings, release, ok := s.aiGate(w, r, workspaceID)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	if err := s.generateDigest(ctx, settings, workspaceID, period, currentUser(r).ID, "manual"); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	response, err := s.buildDigest(r.Context(), workspaceID, period, true)
	if err != nil {
		writeStoreError(w, "could not load digest")
		return
	}
	writeJSON(w, 200, response)
}

const digestTask = `Write a short activity digest for a delivery workspace. Evidence "stats" has deployment counts for the period; "activity:<applicationId>" has per-application outcomes; "attention:<applicationId>" lists applications that need attention now with rule-derived severity and facts; "waiting" lists plans waiting for approval. Return "summary" (two or three sentences covering the period: what shipped, what failed, what needs a decision; no filler) and "findings", at most 8, most important first. Each finding has "section" (one of deployed, failed, attention, waiting), "title" (at most 90 characters), "detail" (at most 250 characters, using only supplied facts), optional "applicationId" (exact id from the evidence) and evidenceIds. Merge routine successful deployments into one "deployed" finding. Do not invent causes for failures; quote the recorded message when present. If nothing happened, say so plainly with an empty findings list.`

func (s *Server) generateDigest(ctx context.Context, settings ai.Settings, workspaceID, period, actor, trigger string) error {
	stats, apps, err := s.computeDigest(ctx, workspaceID, period)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, app := range apps {
		names[app.ID] = app.Name
	}
	evidence := []ai.Evidence{{ID: "stats", Label: "Workspace activity (" + period + ")", URL: "/applications", Data: aiScrub(map[string]any{
		"period": period, "since": stats.Since, "until": stats.Until, "operations": stats.Operations, "succeeded": stats.Succeeded, "failed": stats.Failed, "rollbacks": stats.Rollbacks, "applicationCount": len(apps)})}}
	for _, entry := range stats.Applications {
		evidence = append(evidence, ai.Evidence{ID: "activity:" + entry.ApplicationID, Label: entry.Name, URL: "/applications/" + entry.ApplicationID + "?tab=activity", Data: aiScrub(entry)})
	}
	for _, item := range stats.Attention {
		evidence = append(evidence, ai.Evidence{ID: "attention:" + item.ApplicationID, Label: item.ApplicationName + " needs attention", URL: "/applications/" + item.ApplicationID, Data: aiScrub(item)})
	}
	evidence = append(evidence, ai.Evidence{ID: "waiting", Label: "Plans waiting for approval", URL: "/approvals", Data: aiScrub(stats.Waiting)})
	result, err := ai.Analyze(ctx, settings, digestTask, evidence, nil)
	if err != nil {
		return err
	}
	redactResult(&result)
	sections := map[string]bool{"deployed": true, "failed": true, "attention": true, "waiting": true}
	stored := storedDigest{Summary: clip(result.Summary, 600), Items: []digestItem{}, Limitations: []string{"Counts cover the most recent 30 operations per application.", "Attention items use stored observations that can be up to a minute old."}, Stats: stats}
	for _, finding := range result.Findings {
		if len(stored.Items) >= 8 {
			break
		}
		section := finding.Section
		if !sections[section] {
			section = "attention"
		}
		item := digestItem{Section: section, Title: clip(finding.Title, 90), Detail: clip(finding.Detail, 250), EvidenceIDs: finding.EvidenceIDs}
		if name, ok := names[finding.ApplicationID]; ok {
			item.ApplicationID, item.ApplicationName = finding.ApplicationID, name
		}
		stored.Items = append(stored.Items, item)
	}
	for _, limitation := range result.Limitations {
		if len(stored.Limitations) < 4 {
			stored.Limitations = append(stored.Limitations, clip(limitation, 300))
		}
	}
	if err := s.Store.Audit(ctx, actor, "ai.digest_generated", "workspace", workspaceID, map[string]any{"period": period, "trigger": trigger, "model": settings.Model}); err != nil {
		return err
	}
	raw, _ := json.Marshal(stored)
	return s.Store.SaveAIDigest(ctx, workspaceID, period, store.AIDigestRecord{Model: settings.Model, Result: raw, GeneratedAt: time.Now().UTC()})
}

// autoDigests refreshes the daily digest once a day and the weekly digest once a week.
func (s *Server) autoDigests(ctx context.Context, workspaceID string) {
	settings, err := s.loadAI(ctx)
	if err != nil || !settings.Enabled {
		return
	}
	for _, period := range []string{"day", "week"} {
		window, _ := digestWindow(period)
		record, found, err := s.Store.AIDigest(ctx, workspaceID, period)
		if err != nil || (found && time.Since(record.GeneratedAt) < window) {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
		_ = s.generateDigest(runCtx, settings, workspaceID, period, "", "automatic")
		cancel()
		if ctx.Err() != nil {
			return
		}
	}
}
