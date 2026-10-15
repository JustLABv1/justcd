package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

func ValidateCommentMode(mode string) error {
	switch mode {
	case "", "summary", "action-required", "off":
		return nil
	}
	return errors.New("commentMode must be summary, action-required, or off")
}

// UpdatePRComment serializes updates across applications and worker processes.
// Provider writes happen under the row lock so sections cannot overwrite each other.
// A stable marker recovers a successful provider write if the DB commit failed.
func (s *Store) UpdatePRComment(ctx context.Context, client scm.Client, c SourceControlConnection, number int, token, section, body string, allowCreate bool) error {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", c.WorkspaceID, c.Provider, strings.TrimRight(c.APIURL, "/"), c.Repository, number))))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO pull_request_comments(key,workspace_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, c.WorkspaceID); err != nil {
		return err
	}
	var id int64
	var raw []byte
	var previous string
	if err = tx.QueryRowContext(ctx, `SELECT comment_id,sections,reported_body FROM pull_request_comments WHERE key=$1 FOR UPDATE`, key).Scan(&id, &raw, &previous); err != nil {
		return err
	}
	sections := map[string]string{}
	if err = json.Unmarshal(raw, &sections); err != nil {
		return err
	}
	// In action-only mode an existing summary still reflects recovery. New healthy
	// applications are omitted until they need attention.
	if !allowCreate && sections[section] == "" {
		return tx.Commit()
	}
	if body == "" {
		delete(sections, section)
	} else {
		sections[section] = body
	}
	keys := make([]string, 0, len(sections))
	for k := range sections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	marker := "<!-- justcd-summary:" + key + " -->"
	parts := []string{marker, "## JustCD"}
	for _, k := range keys {
		parts = append(parts, sections[k])
	}
	if len(sections) == 0 {
		parts = append(parts, "No action is required.")
	}
	next := strings.Join(parts, "\n\n") + "\n"
	if next != previous {
		// Do not create a comment merely to clear an absent discovery error.
		if id == 0 && len(sections) == 0 {
			return tx.Commit()
		}
		id, err = client.UpsertComment(ctx, c.Provider, c.APIURL, c.Repository, token, number, id, marker, next)
		if err != nil {
			return err
		}
	}
	raw, err = json.Marshal(sections)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pull_request_comments SET comment_id=$2,sections=$3,reported_body=$4 WHERE key=$1`, key, id, raw, next); err != nil {
		return err
	}
	return tx.Commit()
}
