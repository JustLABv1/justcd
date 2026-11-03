package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/ai"
	"github.com/justlab/justcd/services/backend/internal/store"
)

const (
	triageMaxModelApps = 12
	triageMaxScanApps  = 100
	triageDebounce     = 90 * time.Second
	triageCooldown     = 10 * time.Minute
)

type triageItem struct {
	ApplicationID   string   `json:"applicationId"`
	ApplicationName string   `json:"applicationName"`
	Severity        string   `json:"severity"`
	Category        string   `json:"category"`
	Headline        string   `json:"headline"`
	Detail          string   `json:"detail,omitempty"`
	Impact          string   `json:"impact"`
	Action          string   `json:"action"`
	ActionTab       string   `json:"actionTab"`
	Facts           []string `json:"facts,omitempty"`
	EvidenceIDs     []string `json:"evidenceIds,omitempty"`
}

type storedTriage struct {
	Summary     string       `json:"summary"`
	Items       []triageItem `json:"items"`
	Limitations []string     `json:"limitations"`
}

type triageRun struct {
	Stage   string
	Started time.Time
	Err     string
}
type triagePending struct {
	Hash  string
	Since time.Time
}

type triageResponse struct {
	State          string       `json:"state"` // idle | running | ready | error
	Stage          string       `json:"stage,omitempty"`
	Source         string       `json:"source"` // ai | rules
	Stale          bool         `json:"stale"`
	Available      bool         `json:"available"`
	AutoTriage     bool         `json:"autoTriage"`
	Trigger        string       `json:"trigger,omitempty"`
	Model          string       `json:"model,omitempty"`
	GeneratedAt    *time.Time   `json:"generatedAt,omitempty"`
	Summary        string       `json:"summary,omitempty"`
	Items          []triageItem `json:"items"`
	Limitations    []string     `json:"limitations"`
	AttentionCount int          `json:"attentionCount"`
	Error          string       `json:"error,omitempty"`
}

const triageTask = `Triage which applications need attention first. The evidence has, per application, an "app:<id>" summary, a "signal:<id>" record derived by deterministic rules (category, servingTraffic, rolloutStalled, workloads, facts, issues) and optionally the latest "operation:<id>". Treat the signal as ground truth; you may add context from the other evidence but never invent facts. Rank by real user impact: an application that is down outranks one that still serves traffic while a new rollout fails (for example new pods cannot start while the previous version keeps running); delivery that is merely blocked or paused comes last unless the evidence shows impact. Return one finding per application, most important first. Each finding must also contain: "applicationId" (exact id from the evidence), "severity" (one of critical, high, medium, low; never lower than the signal's severity), "title" (headline, at most 90 characters), "detail" (why this ranks here, at most 300 characters), "impact" (one sentence describing the user-facing effect, e.g. "Still serving the previous version; the new pods cannot start"), "action" (one concrete next step a human should take) and "actionTab" (one of overview, changes, topology, activity, settings). Cite "app:<id>" and "signal:<id>" in evidenceIds. "summary" is one or two sentences naming the single most important application and why.`

func clip(value string, limit int) string {
	value = strings.TrimSpace(redactAPIErrorMessage(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit-1] + "…"
}

func (s *Server) triageInputs(ctx context.Context, workspaceID string) ([]triageSignal, []store.Application, string, error) {
	apps, err := s.Store.ListApplications(ctx, workspaceID)
	if err != nil {
		return nil, nil, "", err
	}
	signals := []triageSignal{}
	byID := []store.Application{}
	for _, app := range apps {
		if !attentionNeeded(app) {
			continue
		}
		if len(signals) >= triageMaxScanApps {
			break
		}
		observed, e := s.Store.ObservedResources(ctx, app.ID)
		if e != nil {
			return nil, nil, "", e
		}
		signals = append(signals, classifyApplication(app, observed))
		byID = append(byID, app)
	}
	sortSignals(signals)
	digest := sha256.New()
	for _, signal := range signals {
		raw, _ := json.Marshal([]any{signal.ApplicationID, signal.Category, signal.Severity, signal.Facts, signal.Issues, signal.SyncStatus, signal.RuntimeStatus})
		digest.Write(raw)
	}
	return signals, byID, hex.EncodeToString(digest.Sum(nil)), nil
}

var ruleText = map[string][3]string{
	// headline, impact, action
	categoryOutage:          {"Down: no replicas are available", "Users cannot reach this application right now.", "Open the failing workload and check its pods and events."},
	categoryDegradedServing: {"Still serving, but pods are failing", "The previous version keeps serving; the new rollout cannot complete.", "Inspect the failing pods before the next deployment."},
	categoryBlockedDelivery: {"Delivery is blocked", "The running version is unaffected, but new changes cannot be applied.", "Resolve the blocking problem, then refresh the plan."},
	categoryNeedsReview:     {"Needs review", "No outage detected; the application differs from its desired state.", "Review the pending changes."},
	categoryPaused:          {"Auto-sync is paused", "The running version is unaffected; Git changes are not being applied.", "Resume auto-sync when ready."},
}

func ruleItem(signal triageSignal) triageItem {
	text := ruleText[signal.Category]
	headline := text[0]
	if signal.Category == categoryBlockedDelivery && len(signal.Issues) > 0 {
		headline = clip(signal.Issues[0], 90)
	}
	return triageItem{ApplicationID: signal.ApplicationID, ApplicationName: signal.Name, Severity: signal.Severity, Category: signal.Category,
		Headline: headline, Impact: text[1], Action: text[2], ActionTab: signal.Tab, Facts: signal.Facts}
}

func ruleItems(signals []triageSignal) []triageItem {
	items := make([]triageItem, 0, len(signals))
	for _, signal := range signals {
		items = append(items, ruleItem(signal))
	}
	return items
}

func (s *Server) currentTriage(ctx context.Context, workspaceID string) (triageResponse, error) {
	signals, _, hash, err := s.triageInputs(ctx, workspaceID)
	if err != nil {
		return triageResponse{}, err
	}
	enabled, err := s.Store.WorkspaceAIEnabled(ctx, workspaceID)
	if err != nil {
		return triageResponse{}, err
	}
	settings, _, err := s.Store.AISettings(ctx)
	if err != nil {
		return triageResponse{}, err
	}
	auto, err := s.Store.WorkspaceAIAutoTriage(ctx, workspaceID)
	if err != nil {
		return triageResponse{}, err
	}
	response := triageResponse{State: "idle", Source: "rules", Available: enabled && settings.Enabled, AutoTriage: auto, Items: ruleItems(signals), Limitations: []string{}, AttentionCount: len(signals)}
	record, found, err := s.Store.AITriage(ctx, workspaceID)
	if err != nil {
		return triageResponse{}, err
	}
	if found {
		var stored storedTriage
		if json.Unmarshal(record.Result, &stored) == nil && (len(stored.Items) > 0 || len(signals) == 0) {
			generated := record.GeneratedAt
			response.GeneratedAt, response.Trigger, response.Model = &generated, record.Trigger, record.Model
			response.Stale = record.InputsHash != hash
			if len(stored.Items) > 0 {
				response.Source, response.Items, response.Summary = "ai", stored.Items, stored.Summary
				if response.Items == nil {
					response.Items = []triageItem{}
				}
			}
			response.Limitations = append(response.Limitations, stored.Limitations...)
			if response.Stale {
				// Keep the rules ordering authoritative for apps that appeared since the AI run.
				response.Items = mergeTriage(stored.Items, signals)
			}
			response.State = "ready"
		}
	}
	s.triageMu.Lock()
	if run := s.triageRuns[workspaceID]; run != nil {
		if run.Err != "" {
			response.Error = run.Err
			response.State = "error"
		} else {
			response.State, response.Stage = "running", run.Stage
		}
	}
	s.triageMu.Unlock()
	return response, nil
}

// mergeTriage keeps AI wording for applications that still need attention,
// drops resolved ones and adds rules-based entries for new ones.
func mergeTriage(stored []triageItem, signals []triageSignal) []triageItem {
	live := map[string]triageSignal{}
	for _, signal := range signals {
		live[signal.ApplicationID] = signal
	}
	seen := map[string]bool{}
	items := []triageItem{}
	for _, item := range stored {
		if signal, ok := live[item.ApplicationID]; ok && !seen[item.ApplicationID] {
			seen[item.ApplicationID] = true
			if severityRank[signal.Severity] < severityRank[item.Severity] {
				item.Severity = signal.Severity
			}
			items = append(items, item)
		}
	}
	for _, signal := range signals {
		if !seen[signal.ApplicationID] {
			items = append(items, ruleItem(signal))
		}
	}
	sortItems(items)
	return items
}

func sortItems(items []triageItem) {
	// Stable so the model's order is kept within a severity.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && severityRank[items[j].Severity] < severityRank[items[j-1].Severity]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func (s *Server) getTriage(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	response, err := s.currentTriage(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load triage")
		return
	}
	writeJSON(w, 200, response)
}

// postTriage starts a background run and returns immediately; clients poll GET for the stage.
func (s *Server) postTriage(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	enabled, err := s.Store.WorkspaceAIEnabled(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load workspace AI policy")
		return
	}
	if !enabled {
		writeError(w, 409, "a workspace owner must enable AI in workspace settings first")
		return
	}
	settings, err := s.loadAI(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI connection")
		return
	}
	if !settings.Enabled {
		writeError(w, 409, "an administrator must configure and enable the AI connection first")
		return
	}
	if !s.beginTriage(workspaceID) {
		response, err := s.currentTriage(r.Context(), workspaceID)
		if err != nil {
			writeStoreError(w, "could not load triage")
			return
		}
		writeJSON(w, 202, response)
		return
	}
	actor := currentUser(r).ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()
		s.finishTriage(workspaceID, s.runTriage(ctx, workspaceID, "manual", actor))
	}()
	response, err := s.currentTriage(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load triage")
		return
	}
	writeJSON(w, 202, response)
}

func (s *Server) beginTriage(workspaceID string) bool {
	s.triageMu.Lock()
	defer s.triageMu.Unlock()
	if s.triageRuns == nil {
		s.triageRuns, s.triageLast = map[string]*triageRun{}, map[string]time.Time{}
	}
	if run := s.triageRuns[workspaceID]; run != nil && run.Err == "" {
		return false
	}
	s.triageRuns[workspaceID] = &triageRun{Stage: "collecting", Started: time.Now()}
	return true
}

func (s *Server) setTriageStage(workspaceID, stage string) {
	s.triageMu.Lock()
	defer s.triageMu.Unlock()
	if run := s.triageRuns[workspaceID]; run != nil {
		run.Stage = stage
	}
}

func (s *Server) finishTriage(workspaceID string, err error) {
	s.triageMu.Lock()
	defer s.triageMu.Unlock()
	s.triageLast[workspaceID] = time.Now()
	if err != nil {
		s.triageRuns[workspaceID] = &triageRun{Err: clip(err.Error(), 300), Started: time.Now()}
		return
	}
	delete(s.triageRuns, workspaceID)
}

var errTriageUnavailable = errors.New("AI is not enabled for this workspace")

func (s *Server) runTriage(ctx context.Context, workspaceID, trigger, actor string) error {
	settings, err := s.loadAI(ctx)
	if err != nil {
		return errors.New("could not load AI connection")
	}
	enabled, err := s.Store.WorkspaceAIEnabled(ctx, workspaceID)
	if err != nil || !enabled || !settings.Enabled {
		return errTriageUnavailable
	}
	signals, apps, hash, err := s.triageInputs(ctx, workspaceID)
	if err != nil {
		return errors.New("could not collect triage evidence")
	}
	save := func(stored storedTriage, model string) error {
		raw, _ := json.Marshal(stored)
		return s.Store.SaveAITriage(ctx, workspaceID, store.AITriageRecord{InputsHash: hash, Trigger: trigger, Model: model, Result: raw, GeneratedAt: time.Now().UTC()})
	}
	if len(signals) == 0 {
		return save(storedTriage{Items: []triageItem{}, Limitations: []string{}}, settings.Model)
	}
	included := signals
	if len(included) > triageMaxModelApps {
		included = signals[:triageMaxModelApps]
	}
	appByID := map[string]store.Application{}
	for _, app := range apps {
		appByID[app.ID] = app
	}
	evidence := []ai.Evidence{{ID: "scope", Label: "Triage scope", URL: "/workspaces/" + workspaceID, Data: map[string]any{
		"workspaceId": workspaceID, "applicationsNeedingAttention": len(signals), "applicationsIncluded": len(included), "collectedAt": time.Now().UTC(),
		"limitations": "Signals come from stored observations and may be up to a minute old. Live events and logs are not included in triage.",
	}}}
	for _, signal := range included {
		app := appByID[signal.ApplicationID]
		base := "/applications/" + app.ID
		evidence = append(evidence,
			ai.Evidence{ID: "app:" + app.ID, Label: app.Name, URL: base, Data: aiScrub(map[string]any{"id": app.ID, "name": app.Name, "cluster": app.ClusterName, "revision": app.Revision, "lastSyncedRevision": app.LastSyncedRevision, "syncStatus": app.Health, "runtimeStatus": app.HealthCondition.Status, "runtimeMessage": app.HealthCondition.Message, "autoSyncPaused": app.AutoSyncPaused})},
			ai.Evidence{ID: "signal:" + app.ID, Label: app.Name + " signals", URL: base + "?tab=topology", Data: aiScrub(signal)})
		if operations, e := s.Store.ListOperations(ctx, app.ID, 1); e == nil && len(operations) > 0 {
			op := operations[0]
			evidence = append(evidence, ai.Evidence{ID: "operation:" + op.ID, Label: "Latest operation of " + app.Name, URL: base + "?tab=activity", Data: aiScrub(map[string]any{"type": op.Type, "status": op.Status, "message": op.Message, "errorCode": op.ErrorCode, "startedAt": op.StartedAt, "finishedAt": op.FinishedAt})})
		}
	}
	s.setTriageStage(workspaceID, "analyzing")
	result, err := ai.Analyze(ctx, settings, triageTask, evidence, nil)
	if err != nil {
		return err
	}
	s.setTriageStage(workspaceID, "saving")
	byApp := map[string]triageSignal{}
	for _, signal := range signals {
		byApp[signal.ApplicationID] = signal
	}
	tabs := map[string]bool{"overview": true, "changes": true, "topology": true, "activity": true, "settings": true}
	items := []triageItem{}
	seen := map[string]bool{}
	for _, finding := range result.Findings {
		signal, ok := byApp[finding.ApplicationID]
		if !ok || seen[finding.ApplicationID] {
			continue
		}
		seen[finding.ApplicationID] = true
		severity := finding.Severity
		if _, valid := severityRank[severity]; !valid || severityRank[severity] > severityRank[signal.Severity] {
			severity = signal.Severity
		}
		tab := finding.ActionTab
		if !tabs[tab] {
			tab = signal.Tab
		}
		fallback := ruleItem(signal)
		item := triageItem{ApplicationID: signal.ApplicationID, ApplicationName: signal.Name, Severity: severity, Category: signal.Category, ActionTab: tab,
			Headline: clip(finding.Title, 90), Detail: clip(finding.Detail, 300), Impact: clip(finding.Impact, 200), Action: clip(finding.Action, 200), Facts: signal.Facts, EvidenceIDs: finding.EvidenceIDs}
		if item.Headline == "" {
			item.Headline = fallback.Headline
		}
		if item.Impact == "" {
			item.Impact = fallback.Impact
		}
		if item.Action == "" {
			item.Action = fallback.Action
		}
		items = append(items, item)
	}
	for _, signal := range signals {
		if !seen[signal.ApplicationID] {
			items = append(items, ruleItem(signal))
		}
	}
	sortItems(items)
	limitations := []string{"Ranking uses stored observations (up to a minute old), not live events or logs."}
	if len(signals) > len(included) {
		limitations = append(limitations, fmt.Sprintf("Only the %d most severe of %d applications were analysed by the model; the rest are ranked by rules.", len(included), len(signals)))
	}
	for _, limitation := range result.Limitations {
		limitations = append(limitations, clip(limitation, 300))
	}
	if err := s.Store.Audit(ctx, actor, "ai.triage_generated", "workspace", workspaceID, map[string]any{"trigger": trigger, "applications": len(signals), "model": settings.Model}); err != nil {
		return errors.New("could not audit AI triage")
	}
	return save(storedTriage{Summary: clip(result.Summary, 400), Items: items, Limitations: limitations}, settings.Model)
}

// RunAITriage refreshes triage in the background for workspaces that opted in.
// It waits for the attention set to stay unchanged for a short time and enforces a cooldown,
// so flapping applications do not cause continuous model calls.
func (s *Server) RunAITriage(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		workspaces, err := s.Store.AutoTriageWorkspaces(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.WarnContext(ctx, "could not list automatic triage workspaces", "error", err)
			}
			continue
		}
		for _, workspaceID := range workspaces {
			if ctx.Err() != nil {
				return
			}
			s.autoTriage(ctx, logger, workspaceID, time.Now())
		}
	}
}

func (s *Server) autoTriage(ctx context.Context, logger *slog.Logger, workspaceID string, now time.Time) {
	_, _, hash, err := s.triageInputs(ctx, workspaceID)
	if err != nil {
		return
	}
	record, found, err := s.Store.AITriage(ctx, workspaceID)
	if err != nil || (found && record.InputsHash == hash) {
		return
	}
	s.triageMu.Lock()
	if s.triagePending == nil {
		s.triagePending = map[string]triagePending{}
	}
	pending := s.triagePending[workspaceID]
	if pending.Hash != hash {
		s.triagePending[workspaceID] = triagePending{Hash: hash, Since: now}
		s.triageMu.Unlock()
		return
	}
	last := s.triageLast[workspaceID]
	s.triageMu.Unlock()
	if now.Sub(pending.Since) < triageDebounce || now.Sub(last) < triageCooldown || !s.beginTriage(workspaceID) {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	err = s.runTriage(runCtx, workspaceID, "automatic", "")
	s.finishTriage(workspaceID, err)
	if err != nil && ctx.Err() == nil {
		logger.WarnContext(ctx, "automatic AI triage failed", "workspaceId", workspaceID, "error", err)
	}
}
