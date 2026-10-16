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
