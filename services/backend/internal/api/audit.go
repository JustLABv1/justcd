package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func parseAuditFilter(r *http.Request) (store.AuditFilter, error) {
	q := r.URL.Query()
	f := store.AuditFilter{WorkspaceID: q.Get("workspaceId"), Group: q.Get("group"), Query: strings.TrimSpace(q.Get("q")), ResourceID: q.Get("resourceId"), ActorID: q.Get("actorId")}
	if f.Group == "" {
		f.Group = "all"
	}
	if f.Group != "all" && f.Group != "changes" && f.Group != "deployments" && f.Group != "access" {
		return f, errors.New("group must be all, changes, deployments, or access")
	}
	if len(f.Query) > 500 {
		return f, errors.New("search must be at most 500 characters")
	}
	if v := q.Get("eventId"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return f, errors.New("eventId must be a positive audit event ID")
		}
		f.EventID = n
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return f, errors.New("before must be a positive audit event ID")
		}
		f.Before = n
	}
	for _, item := range []struct {
		key  string
		dest **time.Time
	}{{"from", &f.From}, {"until", &f.Until}} {
		if v := q.Get(item.key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, fmt.Errorf("%s must be an RFC3339 timestamp", item.key)
			}
			*item.dest = &t
		}
	}
	if f.From != nil && f.Until != nil && !f.From.Before(*f.Until) {
		return f, errors.New("from must be earlier than until")
	}
	return f, nil
}
func (s *Server) authorizeAudit(w http.ResponseWriter, r *http.Request, workspace string) bool {
	if currentUser(r).IsAdmin {
		return true
	}
	if workspace == "" {
		writeError(w, http.StatusForbidden, "instance administrator role or workspaceId is required")
		return false
	}
	return s.requireWorkspaceRole(w, r, workspace, "viewer")
}
func safeAuditView(e store.AuditEvent) store.AuditEvent {
	var details map[string]any
	if json.Unmarshal(e.Details, &details) == nil {
		e.Details, _ = json.Marshal(redactAPIErrorDetails(details))
	} else {
		e.Details = json.RawMessage(`{}`)
	}
	return e
}
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	f, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !s.authorizeAudit(w, r, f.WorkspaceID) {
		return
	}
	items, err := s.Store.QueryAuditEvents(r.Context(), f, 51)
	if err != nil {
		writeStoreError(w, "could not load audit events")
		return
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	for i := range items {
		items[i] = safeAuditView(items[i])
	}
	writeJSON(w, 200, map[string]any{"schemaVersion": store.AuditSchemaVersion, "items": items, "hasMore": more})
}
func (s *Server) auditDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("eventID"), 10, 64)
	if err != nil || id <= 0 || id == int64(^uint64(0)>>1) {
		writeError(w, 400, "invalid event ID")
		return
	}
	e, err := s.Store.AuditEventByID(r.Context(), id)
	if err != nil {
		writeError(w, 404, "audit event not found")
		return
	}
	if !s.authorizeAudit(w, r, e.WorkspaceID) {
		return
	}
	writeJSON(w, 200, safeAuditView(e))
}

type auditExportRecord struct {
	store.AuditEvent
	SHA256 string `json:"sha256"`
}

func exportAuditRecord(e store.AuditEvent) auditExportRecord {
	e = store.AuditExportEvent(safeAuditView(e))
	raw, _ := json.Marshal(e)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var canonical any
	_ = decoder.Decode(&canonical)
	raw, _ = json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return auditExportRecord{e, hex.EncodeToString(sum[:])}
}
func csvAuditValue(v string) string {
	trimmed := strings.TrimLeft(v, " \t\r\n")
	if (len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))) || strings.HasPrefix(v, "\t") || strings.HasPrefix(v, "\r") {
		return "'" + v
	}
	return v
}
func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request) {
	f, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !s.authorizeAudit(w, r, f.WorkspaceID) {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeError(w, 400, "format must be json or csv")
		return
	}
	// A fixed high-water ID prevents new events extending an export indefinitely.
	items, err := s.Store.QueryAuditEvents(r.Context(), f, 500)
	if err != nil {
		writeStoreError(w, "could not export audit events")
		return
	}
	if len(items) > 0 {
		f.Through = items[0].ID
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="justcd-audit.`+format+`"`)
	w.Header().Set("Trailer", "X-Audit-Export-Complete")
	var csvWriter *csv.Writer
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		csvWriter = csv.NewWriter(w)
		err = csvWriter.Write([]string{"schemaVersion", "id", "workspaceId", "actorId", "actorName", "action", "resourceType", "resourceId", "resourceName", "createdAt", "details", "deploymentRecord", "sha256"})
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, err = fmt.Fprintf(w, `{"schemaVersion":1,"exportedAt":%q,"items":[`, time.Now().UTC().Format(time.RFC3339Nano))
	}
	first := true
	for err == nil {
		for _, e := range items {
			record := exportAuditRecord(e)
			if csvWriter != nil {
				actor := ""
				if e.ActorID != nil {
					actor = *e.ActorID
				}
				values := []string{strconv.Itoa(record.SchemaVersion), strconv.FormatInt(e.ID, 10), e.WorkspaceID, actor, e.ActorName, e.Action, e.ResourceType, e.ResourceID, e.ResourceName, e.CreatedAt.UTC().Format(time.RFC3339Nano), string(record.Details), string(record.DeploymentRecord), record.SHA256}
				for i := range values {
					values[i] = csvAuditValue(values[i])
				}
				err = csvWriter.Write(values)
			} else {
				if !first {
					_, err = fmt.Fprint(w, ",")
				}
				if err == nil {
					err = json.NewEncoder(w).Encode(record)
				}
				first = false
			}
			if err != nil {
				return
			}
		}
		if csvWriter != nil {
			csvWriter.Flush()
			if csvWriter.Error() != nil {
				return
			}
		}
		if len(items) < 500 {
			break
		}
		if err = http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return
		}
		f.Before = items[len(items)-1].ID
		items, err = s.Store.QueryAuditEvents(r.Context(), f, 500)
	}
	if err != nil {
		panic(http.ErrAbortHandler) // A failed query must not look like a completed CSV download.
	}
	if csvWriter == nil {
		if _, err = fmt.Fprint(w, `],"complete":true}`); err != nil {
			return
		}
	}
	w.Header().Set("X-Audit-Export-Complete", "true")
}
func (s *Server) getAuditRetention(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "viewer") {
		return
	}
	value, err := s.Store.AuditRetention(r.Context(), workspace)
	if err != nil {
		writeError(w, 404, "workspace not found")
		return
	}
	writeJSON(w, 200, value)
}
func (s *Server) setAuditRetention(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	var input struct {
		Days *int `json:"days"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Days == nil || *input.Days < 0 || *input.Days > 36500 {
		writeError(w, 400, "days must be between 0 (keep indefinitely) and 36500")
		return
	}
	if err := s.Store.SetAuditRetention(r.Context(), workspace, currentUser(r).ID, *input.Days); err != nil {
		writeStoreError(w, "could not update audit retention")
		return
	}
	writeJSON(w, 200, store.AuditRetention{Days: *input.Days})
}
