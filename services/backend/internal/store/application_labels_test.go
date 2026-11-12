package store

import "testing"

func TestApplicationLabelsValidateKubernetesStyleMetadata(t *testing.T) {
	for _, labels := range []map[string]string{nil, {}, {"team": "platform", "example.com/environment": "production", "empty": ""}} {
		if err := ValidateApplicationLabels(labels); err != nil {
			t.Fatalf("valid labels rejected: %v", err)
		}
	}
	for _, labels := range []map[string]string{{"": "value"}, {"bad key": "value"}, {"team": "bad value"}, {"team": "<script>"}} {
		if err := ValidateApplicationLabels(labels); err == nil {
			t.Fatalf("invalid labels accepted: %+v", labels)
		}
	}
	labels := map[string]string{}
	for i := range 33 {
		labels[string(rune('a'+i))] = "value"
	}
	if err := ValidateApplicationLabels(labels); err == nil {
		t.Fatal("more than 32 labels accepted")
	}
}

func TestApplicationSavedViewValidation(t *testing.T) {
	config := ApplicationViewConfig{GroupBy: "label", LabelKey: "team", Layout: "cards", Sort: "name", Status: "all"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.LabelKey = ""
	if err := config.Validate(); err == nil {
		t.Fatal("label grouping accepted without a label key")
	}
	config.LabelKey = "team"
	config.Layout = "unknown"
	if err := config.Validate(); err == nil {
		t.Fatal("invalid layout accepted")
	}
}

func TestApplicationSavedViewAdvancedFilters(t *testing.T) {
	config := ApplicationViewConfig{GroupBy: "none", Layout: "cards", Sort: "name", Status: "all", Filters: []byte(`{"id":"g","type":"group","combinator":"and","rules":[
		{"id":"a","type":"rule","path":["name"],"operator":"contains","value":"bill"},
		{"id":"b","type":"rule","path":["status"],"operator":"is_any_of","value":["attention","other"]},
		{"id":"c","type":"rule","path":["cluster"],"operator":"not_empty"},
		{"id":"d","type":"rule","path":["lastChecked"],"operator":"is_after","value":"2026-01-01T10:00","negated":true},
		{"id":"e","type":"rule","path":["label"],"operator":"","value":null}]}`)}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Filters = []byte(`{"id":"g","type":"group","combinator":"and","rules":[{"id":"a","type":"rule","path":["secret"],"operator":"is","value":"x"}]}`)
	if err := config.Validate(); err == nil {
		t.Fatal("unknown filter field accepted")
	}
}
