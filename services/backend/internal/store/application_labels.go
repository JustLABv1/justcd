package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

// App labels organize JustCD applications; they are not added to Kubernetes objects.
func ValidateApplicationLabels(labels map[string]string) error {
	if len(labels) > 32 {
		return errors.New("applications support at most 32 labels")
	}
	for key, value := range labels {
		if problems := validation.IsQualifiedName(key); len(problems) > 0 {
			return fmt.Errorf("invalid label key %q: %s", key, problems[0])
		}
		if problems := validation.IsValidLabelValue(value); len(problems) > 0 {
			return fmt.Errorf("invalid value for label %q: %s", key, problems[0])
		}
	}
	return nil
}

func applicationLabelsJSON(labels map[string]string) ([]byte, error) {
	if err := ValidateApplicationLabels(labels); err != nil {
		return nil, err
	}
	if labels == nil {
		labels = map[string]string{}
	}
	return json.Marshal(labels)
}

func (s *Store) SetApplicationLabels(ctx context.Context, id string, labels map[string]string) error {
	raw, err := applicationLabelsJSON(labels)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE applications SET labels=$2,updated_at=NOW() WHERE id=$1 AND repository_configuration_id IS NULL AND branch_test IS NULL`, id, raw)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return errors.New("application labels are managed by Git or a branch test")
	}
	return err
}
