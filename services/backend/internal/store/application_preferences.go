package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrApplicationViewLimit = errors.New("you can save up to 20 application views")

type ApplicationViewConfig struct {
	GroupBy       string          `json:"groupBy"`
	LabelKey      string          `json:"labelKey,omitempty"`
	Query         string          `json:"query"`
	Status        string          `json:"status"`
	ClusterID     string          `json:"clusterId"`
	Layout        string          `json:"layout"`
	Sort          string          `json:"sort"`
	FavoritesOnly bool            `json:"favoritesOnly"`
	Filters       json.RawMessage `json:"filters,omitempty"`
}

type ApplicationSavedView struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Configuration ApplicationViewConfig `json:"configuration"`
	IsDefault     bool                  `json:"isDefault"`
}

func (c ApplicationViewConfig) Validate() error {
	allowed := func(value string, options ...string) bool {
		for _, option := range options {
			if value == option {
				return true
			}
		}
		return false
	}
	if !allowed(c.GroupBy, "cluster", "namespace", "status", "none", "label") || !allowed(c.Layout, "cards", "list") || !allowed(c.Sort, "attention", "name", "newest") || !allowed(c.Status, "all", "attention", "synced", "other", "paused") {
		return errors.New("invalid application view grouping, layout, sort or status")
	}
	if c.GroupBy == "label" && len(validation.IsQualifiedName(c.LabelKey)) > 0 {
		return errors.New("choose a valid label key to group by")
	}
	if len(c.Query) > 512 || len(c.ClusterID) > 128 || len(c.LabelKey) > 317 || len(c.Filters) > 32768 {
		return errors.New("application view exceeds size limits")
	}
	if len(c.Filters) > 0 && string(c.Filters) != "null" {
		var node viewFilter
		if err := json.Unmarshal(c.Filters, &node); err != nil {
			return errors.New("invalid application view filters")
		}
		count := 0
		if err := node.validate(0, &count); err != nil {
			return err
		}
	}
	return nil
}

type viewFilter struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Path       []string        `json:"path"`
	Operator   string          `json:"operator"`
	Combinator string          `json:"combinator"`
	Value      json.RawMessage `json:"value"`
	Rules      []viewFilter    `json:"rules"`
}

func (f viewFilter) validate(depth int, count *int) error {
	*count += 1
	if depth > 5 || *count > 100 || f.ID == "" || len(f.ID) > 128 {
		return errors.New("invalid application view filter structure")
	}
	if f.Type == "group" {
		if f.Rules == nil || (f.Combinator != "and" && f.Combinator != "or") {
			return errors.New("invalid filter combinator")
		}
		for _, rule := range f.Rules {
			if err := rule.validate(depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	if depth == 0 || f.Type != "rule" || len(f.Path) != 1 || f.Operator != "is" {
		return errors.New("invalid application view filter")
	}
	if len(f.Value) > 0 && string(f.Value) != "null" {
		var value any
		if err := json.Unmarshal(f.Value, &value); err != nil {
			return errors.New("invalid filter value")
		}
		switch value := value.(type) {
		case string:
			if len(value) > 512 {
				return errors.New("filter value is too long")
			}
		case []any:
			if len(value) > 100 {
				return errors.New("too many filter values")
			}
			for _, item := range value {
				text, ok := item.(string)
				if !ok || len(text) > 512 {
					return errors.New("invalid filter value")
				}
			}
		default:
			return errors.New("filter values must be strings")
		}
	}
	field := f.Path[0]
	if field != "workspace" && field != "namespace" && field != "reconciliation" && field != "renderer" && !(strings.HasPrefix(field, "label:") && len(validation.IsQualifiedName(strings.TrimPrefix(field, "label:"))) == 0) {
		return errors.New("unknown application view filter field")
	}
	return nil
}

func (s *Store) ApplicationSavedViews(ctx context.Context, userID string) ([]ApplicationSavedView, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,configuration,is_default FROM application_saved_views WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := []ApplicationSavedView{}
	for rows.Next() {
		var view ApplicationSavedView
		var raw []byte
		if err := rows.Scan(&view.ID, &view.Name, &raw, &view.IsDefault); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &view.Configuration); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func (s *Store) SaveApplicationView(ctx context.Context, userID string, view ApplicationSavedView, create bool) (ApplicationSavedView, error) {
	view.Name = strings.TrimSpace(view.Name)
	if view.Name == "" || len(view.Name) > 80 {
		return view, errors.New("view name must be between 1 and 80 characters")
	}
	if err := view.Configuration.Validate(); err != nil {
		return view, err
	}
	raw, err := json.Marshal(view.Configuration)
	if err != nil {
		return view, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return view, err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id); err != nil {
		return view, err
	}
	if create {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_saved_views WHERE user_id=$1`, userID).Scan(&count); err != nil {
			return view, err
		}
		if count >= 20 {
			return view, ErrApplicationViewLimit
		}
		view.ID = NewID()
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application_saved_views WHERE id=$1 AND user_id=$2`, view.ID, userID).Scan(&id); err != nil {
			return view, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_saved_views SET is_default=FALSE WHERE user_id=$1`, userID); err != nil {
		return view, err
	}
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO application_saved_views(id,user_id,name,configuration,is_default) VALUES($1,$2,$3,$4,TRUE)`, view.ID, userID, view.Name, raw)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE application_saved_views SET name=$3,configuration=$4,is_default=TRUE WHERE id=$1 AND user_id=$2`, view.ID, userID, view.Name, raw)
	}
	if err != nil {
		return view, err
	}
	view.IsDefault = true
	return view, tx.Commit()
}

func (s *Store) SetDefaultApplicationView(ctx context.Context, userID, viewID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id); err != nil {
		return err
	}
	if viewID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application_saved_views WHERE user_id=$1 AND id=$2`, userID, viewID).Scan(&id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_saved_views SET is_default=FALSE WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if viewID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE application_saved_views SET is_default=TRUE WHERE user_id=$1 AND id=$2`, userID, viewID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteApplicationView(ctx context.Context, userID, viewID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM application_saved_views WHERE user_id=$1 AND id=$2`, userID, viewID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) ApplicationFavorites(ctx context.Context, user User) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.application_id FROM application_favorites f JOIN applications a ON a.id=f.application_id WHERE f.user_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM workspace_memberships m WHERE m.workspace_id=a.workspace_id AND m.user_id=$1) OR EXISTS(SELECT 1 FROM oidc_membership_grants g WHERE g.workspace_id=a.workspace_id AND g.user_id=$1)) ORDER BY f.application_id`, user.ID, user.IsAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) SetApplicationFavorite(ctx context.Context, userID, appID string, favorite bool) error {
	if favorite {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO application_favorites(user_id,application_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, appID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM application_favorites WHERE user_id=$1 AND application_id=$2`, userID, appID)
	return err
}
