package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/ai"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/syncer"
)

type conflictGroup struct {
	Key            string          `json:"key"`
	Category       string          `json:"category"` // orphaned | other_application | controller_child | unmanaged
	Title          string          `json:"title"`
	Owner          string          `json:"owner,omitempty"`
	Recommendation string          `json:"recommendation"` // adopt | leave | exclude | investigate
	Safe           bool            `json:"safe"`           // adoption changes nothing live
	Reason         string          `json:"reason"`
	Impact         string          `json:"impact"`
	Count          int             `json:"count"`
	Matching       int             `json:"matching"` // resources identical to the desired state
	Kinds          map[string]int  `json:"kinds"`
	Identities     []core.Identity `json:"identities"`
	Source         string          `json:"source"` // rules | ai
}

type conflictGuidance struct {
	ApplicationID string          `json:"applicationId"`
	Source        string          `json:"source"`
	Available     bool            `json:"available"`
	Summary       string          `json:"summary,omitempty"`
	Groups        []conflictGroup `json:"groups"`
	Model         string          `json:"model,omitempty"`
	GeneratedAt   *time.Time      `json:"generatedAt,omitempty"`
}

// groupConflicts classifies conflicts deterministically. Adoption is only suggested when
// no other JustCD application or controller still owns the resource.
func (s *Server) groupConflicts(ctx context.Context, conflicts []*syncer.OwnershipConflict) []conflictGroup {
	groups := map[string]*conflictGroup{}
	names := map[string]string{}
	ownerName := func(id string) string {
		if name, ok := names[id]; ok {
			return name
		}
		name := id
		if app, err := s.Store.ApplicationByID(ctx, id); err == nil {
			name = app.Name
		}
		names[id] = name
		return name
	}
	for _, conflict := range conflicts {
		var category, title, recommendation, reason, impact, owner string
		switch {
		case conflict.Owner != "" && !conflict.OwnerMissing:
			owner = ownerName(conflict.Owner)
			category, title, recommendation = "other_application", "Managed by application "+owner, "leave"
			reason = "Another JustCD application currently owns these resources."
			impact = "Adopting moves ownership; both applications would then fight over the same resources unless the other one releases them."
		case conflict.Owner != "" && conflict.OwnerMissing:
			category, title, recommendation = "orphaned", "Left behind by a deleted application", "adopt"
			reason = "The application that created these resources no longer exists, so nothing else reconciles them."
			impact = "Adopting makes this application the owner and applies its desired state."
		case conflict.HasOwnerReferences:
			category, title, recommendation = "controller_child", "Created by another controller", "exclude"
			reason = "These resources are owned by a Kubernetes controller or operator that recreates them."
			impact = "Adopting would conflict with that controller; exclude them from this application's configuration instead."
		default:
			category, title, recommendation = "unmanaged", "Created outside JustCD", "adopt"
			reason = "No JustCD application or controller owner was found (created by hand, by Helm or by another tool)."
			impact = "Adopting hands management to this application. Make sure no other tool (for example Helm or another GitOps controller) still reconciles them."
		}
		key := category + ":" + owner
		group := groups[key]
		if group == nil {
			group = &conflictGroup{Key: key, Category: category, Title: title, Owner: owner, Recommendation: recommendation, Reason: reason, Impact: impact, Kinds: map[string]int{}, Identities: []core.Identity{}, Source: "rules"}
			groups[key] = group
		}
		group.Count++
		group.Kinds[conflict.Identity.Kind]++
		if conflict.LiveFingerprint != "" && conflict.LiveFingerprint == conflict.DesiredFingerprint {
			group.Matching++
		}
		if len(group.Identities) < 200 {
			group.Identities = append(group.Identities, conflict.Identity)
		}
	}
	result := make([]conflictGroup, 0, len(groups))
	for _, group := range groups {
		group.Safe = group.Recommendation == "adopt" && group.Matching == group.Count
		if group.Safe {
			group.Impact = "All resources already match the desired state, so adopting changes nothing live."
		} else if group.Recommendation == "adopt" && group.Matching > 0 {
			group.Impact += fmt.Sprintf(" %d of %d already match the desired state.", group.Matching, group.Count)
		} else if group.Recommendation == "adopt" {
			group.Impact += " None match the desired state, so adopting will overwrite live differences; review the diff first."
		}
		result = append(result, *group)
	}
	rank := map[string]int{"adopt": 0, "investigate": 1, "exclude": 2, "leave": 3}
	sort.Slice(result, func(i, j int) bool {
		if rank[result[i].Recommendation] != rank[result[j].Recommendation] {
			return rank[result[i].Recommendation] < rank[result[j].Recommendation]
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func (s *Server) conflictApplication(w http.ResponseWriter, r *http.Request) (store.Application, []*syncer.OwnershipConflict, bool) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return app, nil, false
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return app, nil, false
	}
	conflicts, err := s.Syncer.OwnershipConflicts(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not inspect the current plan")
		return app, nil, false
	}
	return app, conflicts, true
}

func (s *Server) getConflictGuidance(w http.ResponseWriter, r *http.Request) {
	app, conflicts, ok := s.conflictApplication(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, conflictGuidance{ApplicationID: app.ID, Source: "rules", Available: s.aiAvailable(r, app.WorkspaceID), Groups: s.groupConflicts(r.Context(), conflicts)})
}

const conflictTask = `Advise an owner on existing Kubernetes resources that block an application from syncing. Evidence "group:<key>" describes each group of conflicting resources: its category (orphaned, other_application, controller_child, unmanaged), owner, kinds, how many already match the desired state, and a rule-based recommendation. The rule-based recommendation is a safety boundary: you may explain, add nuance and set a clearer action, but never recommend "adopt" for a group whose rule recommendation is leave or exclude. For each group return one finding with "key" (exact group key), "recommendation" (adopt, leave, exclude or investigate), "title" (at most 80 characters), "detail" (why, at most 250 characters, using resource kinds and counts from the evidence) and "impact" (what happens if the user follows it, at most 200 characters). "summary" is two sentences giving the order in which to resolve the groups. Cite "group:<key>" in evidenceIds.`

func (s *Server) postConflictGuidance(w http.ResponseWriter, r *http.Request) {
	app, conflicts, ok := s.conflictApplication(w, r)
	if !ok {
		return
	}
	settings, release, ok := s.aiGate(w, r, app.WorkspaceID)
	if !ok {
		return
	}
	defer release()
	groups := s.groupConflicts(r.Context(), conflicts)
	guidance := conflictGuidance{ApplicationID: app.ID, Source: "rules", Available: true, Groups: groups}
	if len(groups) == 0 {
		writeJSON(w, 200, guidance)
		return
	}
	evidence := []ai.Evidence{}
	for _, group := range groups {
		names := []string{}
		for _, identity := range group.Identities {
			if len(names) < 8 {
				names = append(names, identity.Kind+" "+identity.Name)
			}
		}
		evidence = append(evidence, ai.Evidence{ID: "group:" + group.Key, Label: group.Title, URL: "/applications/" + app.ID, Data: aiScrub(map[string]any{
			"category": group.Category, "owner": group.Owner, "ruleRecommendation": group.Recommendation, "count": group.Count, "alreadyMatchDesired": group.Matching, "kinds": group.Kinds, "examples": names, "ruleReason": group.Reason})})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	result, err := ai.Analyze(ctx, settings, conflictTask, evidence, nil)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	redactResult(&result)
	allowed := map[string]bool{"adopt": true, "leave": true, "exclude": true, "investigate": true}
	byKey := map[string]ai.Finding{}
	for _, finding := range result.Findings {
		byKey[strings.TrimPrefix(finding.Key, "group:")] = finding
	}
	for i := range guidance.Groups {
		group := &guidance.Groups[i]
		finding, ok := byKey[group.Key]
		if !ok {
			continue
		}
		recommendation := finding.Recommendation
		if !allowed[recommendation] || (recommendation == "adopt" && group.Recommendation != "adopt") {
			recommendation = group.Recommendation
		}
		group.Recommendation, group.Source = recommendation, "ai"
		if text := clip(finding.Title, 80); text != "" {
			group.Title = text
		}
		if text := clip(finding.Detail, 250); text != "" {
			group.Reason = text
		}
		if text := clip(finding.Impact, 200); text != "" && !group.Safe {
			group.Impact = text
		}
		if recommendation != "adopt" {
			group.Safe = false
		}
	}
	now := time.Now().UTC()
	guidance.Source, guidance.Summary, guidance.Model, guidance.GeneratedAt = "ai", clip(result.Summary, 400), settings.Model, &now
	if err := s.Store.Audit(r.Context(), currentUser(r).ID, "ai.conflict_guidance_requested", "application", app.ID, map[string]any{"groups": len(groups), "model": settings.Model}); err != nil {
		writeStoreError(w, "could not audit AI request")
		return
	}
	writeJSON(w, 200, guidance)
}
