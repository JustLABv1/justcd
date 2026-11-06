package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AuditCondition is one node of a user-built filter: either a group of nodes joined by
// AND/OR, or a single comparison. Only whitelisted fields and operators are accepted and
// every value travels as a bind parameter, so a filter can never change the query shape.
type AuditCondition struct {
	Type       string           `json:"type"`
	Combinator string           `json:"combinator,omitempty"`
	Rules      []AuditCondition `json:"rules,omitempty"`
	Field      string           `json:"field,omitempty"`
	Operator   string           `json:"operator,omitempty"`
	Value      json.RawMessage  `json:"value,omitempty"`
	Negated    bool             `json:"negated,omitempty"`
}

const (
	maxAuditFilterBytes = 16 << 10
	maxAuditFilterRules = 30
	maxAuditFilterDepth = 4
	maxAuditFilterValue = 200
	maxAuditFilterList  = 50
)

type auditField struct {
	kind string // text, array or date
	expr string // SQL expression, or the JSON key inside a target for arrays
}

var auditFields = map[string]auditField{
	"action":       {"text", `e.action`},
	"actor":        {"text", `COALESCE(NULLIF(u.display_name,''),u.email,e.actor_id,'')`},
	"actorKind":    {"text", `CASE WHEN e.actor_id='justcd-system' THEN 'automation' WHEN e.actor_id IS NOT NULL THEN 'people' ELSE 'other' END`},
	"resourceType": {"text", `e.resource_type`},
	"resourceName": {"text", `e.resource_name`},
	"resourceId":   {"text", `e.resource_id`},
	"workspace":    {"text", `e.workspace_id`},
	"status":       {"text", `CASE WHEN e.action LIKE '%.failed' OR e.action LIKE '%\_failed' THEN 'failed' WHEN e.action LIKE '%.succeeded' THEN 'succeeded' WHEN e.action LIKE '%.queued' THEN 'queued' WHEN e.action LIKE '%.cancelled' THEN 'cancelled' ELSE '' END`},
	"cluster":      {"array", `clusterName`},
	"namespace":    {"array", `namespace`},
	"createdAt":    {"date", `e.created_at`},
}

var auditTextOperators = map[string]bool{"contains": true, "not_contains": true, "starts_with": true, "ends_with": true, "is": true, "is_not": true, "is_any_of": true, "is_none_of": true, "empty": true, "not_empty": true}
var auditDateOperators = map[string]bool{"before": true, "from": true}

// ParseAuditCondition decodes and validates a filter tree. An empty string means no filter.
func ParseAuditCondition(raw string) (*AuditCondition, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > maxAuditFilterBytes {
		return nil, fmt.Errorf("filter is too large")
	}
	var root AuditCondition
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("filter is not valid JSON")
	}
	count := 0
	if err := validateAuditCondition(&root, 1, &count); err != nil {
		return nil, err
	}
	return &root, nil
}

func validateAuditCondition(node *AuditCondition, depth int, count *int) error {
	if depth > maxAuditFilterDepth {
		return fmt.Errorf("filter groups are nested too deeply")
	}
	switch node.Type {
	case "group":
		if node.Combinator != "and" && node.Combinator != "or" {
			return fmt.Errorf("filter group combinator must be and or or")
		}
		for index := range node.Rules {
			if err := validateAuditCondition(&node.Rules[index], depth+1, count); err != nil {
				return err
			}
		}
	case "rule":
		*count++
		if *count > maxAuditFilterRules {
			return fmt.Errorf("filter has too many conditions")
		}
		field, ok := auditFields[node.Field]
		if !ok {
			return fmt.Errorf("unknown filter field %q", node.Field)
		}
		if field.kind == "date" {
			if !auditDateOperators[node.Operator] {
				return fmt.Errorf("operator %q is not valid for %s", node.Operator, node.Field)
			}
			if _, err := auditTime(node.Value); err != nil {
				return fmt.Errorf("%s needs an RFC3339 timestamp", node.Field)
			}
			return nil
		}
		if !auditTextOperators[node.Operator] {
			return fmt.Errorf("operator %q is not valid for %s", node.Operator, node.Field)
		}
		if _, err := auditStrings(node); err != nil {
			return err
		}
	default:
		return fmt.Errorf("filter node type must be group or rule")
	}
	return nil
}

func auditTime(raw json.RawMessage) (time.Time, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, text)
}

// auditStrings returns the operand list: none for empty/not_empty, many for is_any_of, otherwise one.
func auditStrings(node *AuditCondition) ([]string, error) {
	switch node.Operator {
	case "empty", "not_empty":
		return nil, nil
	case "is_any_of", "is_none_of":
		var values []string
		if err := json.Unmarshal(node.Value, &values); err != nil || len(values) == 0 {
			return nil, fmt.Errorf("%s needs at least one value", node.Operator)
		}
		if len(values) > maxAuditFilterList {
			return nil, fmt.Errorf("%s accepts at most %d values", node.Operator, maxAuditFilterList)
		}
		for _, value := range values {
			if len(value) > maxAuditFilterValue {
				return nil, fmt.Errorf("filter values must be at most %d characters", maxAuditFilterValue)
			}
		}
		return values, nil
	default:
		var value string
		if err := json.Unmarshal(node.Value, &value); err != nil || value == "" {
			return nil, fmt.Errorf("%s needs a value", node.Operator)
		}
		if len(value) > maxAuditFilterValue {
			return nil, fmt.Errorf("filter values must be at most %d characters", maxAuditFilterValue)
		}
		return []string{value}, nil
	}
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// buildAuditPredicate renders the tree as SQL, appending bind values to args. Placeholders
// continue from the length of args, so callers can chain it after their own parameters.
func buildAuditPredicate(node *AuditCondition, args *[]any) string {
	if node == nil {
		return "TRUE"
	}
	var sql string
	if node.Type == "group" {
		parts := make([]string, 0, len(node.Rules))
		for index := range node.Rules {
			parts = append(parts, buildAuditPredicate(&node.Rules[index], args))
		}
		if len(parts) == 0 {
			return "TRUE"
		}
		sql = "(" + strings.Join(parts, " "+strings.ToUpper(node.Combinator)+" ") + ")"
	} else {
		sql = buildAuditRule(node, args)
	}
	if node.Negated {
		return "NOT " + sql
	}
	return sql
}

func buildAuditRule(node *AuditCondition, args *[]any) string {
	field := auditFields[node.Field]
	bind := func(value any) string {
		*args = append(*args, value)
		return fmt.Sprintf("$%d", len(*args))
	}
	if field.kind == "date" {
		when, _ := auditTime(node.Value)
		if node.Operator == "before" {
			return field.expr + " < " + bind(when) + "::timestamptz"
		}
		return field.expr + " >= " + bind(when) + "::timestamptz"
	}
	values, _ := auditStrings(node)
	if field.kind == "array" {
		return buildAuditArrayRule(field.expr, node.Operator, values, bind)
	}
	return buildAuditText("COALESCE(("+field.expr+"),'')", node.Operator, values, bind)
}

// buildAuditText renders one comparison against a text expression.
func buildAuditText(expr, operator string, values []string, bind func(any) string) string {
	switch operator {
	case "empty":
		return expr + "=''"
	case "not_empty":
		return expr + "<>''"
	case "contains":
		return expr + ` ILIKE '%' || ` + bind(escapeLike(values[0])) + ` || '%' ESCAPE '\'`
	case "not_contains":
		return "NOT (" + buildAuditText(expr, "contains", values, bind) + ")"
	case "starts_with":
		return expr + ` ILIKE ` + bind(escapeLike(values[0])) + ` || '%' ESCAPE '\'`
	case "ends_with":
		return expr + ` ILIKE '%' || ` + bind(escapeLike(values[0])) + ` ESCAPE '\'`
	case "is":
		return "lower(" + expr + ")=lower(" + bind(values[0]) + ")"
	case "is_not":
		return "lower(" + expr + ")<>lower(" + bind(values[0]) + ")"
	case "is_any_of", "is_none_of":
		lowered := make([]string, len(values))
		for index, value := range values {
			lowered[index] = strings.ToLower(value)
		}
		clause := "lower(" + expr + ")=ANY(" + bind(lowered) + "::text[])"
		if operator == "is_none_of" {
			return "NOT (" + clause + ")"
		}
		return clause
	}
	return "FALSE"
}

// buildAuditArrayRule matches against the resolved targets of an event. Negative operators
// mean that no target matches, which is the only reading that makes sense for several targets.
func buildAuditArrayRule(key, operator string, values []string, bind func(any) string) string {
	positive := map[string]string{"not_contains": "contains", "is_not": "is", "is_none_of": "is_any_of", "empty": "not_empty"}
	wrap := func(op string) string {
		inner := buildAuditText("COALESCE(x->>'"+key+"','')", op, values, bind)
		return "EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(tg.targets,'[]'::jsonb)) x WHERE " + inner + ")"
	}
	if op, negative := positive[operator]; negative {
		if operator == "empty" {
			return "NOT " + wrap("not_empty")
		}
		return "NOT " + wrap(op)
	}
	return wrap(operator)
}
