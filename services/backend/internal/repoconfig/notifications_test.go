package repoconfig

import (
	"strings"
	"testing"
)

func TestNotificationDefinitions(t *testing.T) {
	root := t.TempDir()
	config := validDefinition + `  notifications:
    mode: custom
    destinations: ["Production Telegram", "destination-id"]
    events: [sync_failed, unhealthy]
    minimumSeverity: error
`
	writeDefinition(t, root, "justcd.yaml", config)
	defs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p := defs[0].Spec.Notifications
	if p == nil || p.Mode != "custom" || len(p.Destinations) != 2 || p.MinimumSeverity != "error" {
		t.Fatalf("lost notification policy %+v", p)
	}
	hash := defs[0].Hash
	writeDefinition(t, root, "justcd.yaml", strings.Replace(config, "minimumSeverity: error", "minimumSeverity: warning", 1))
	defs, err = Discover(root)
	if err != nil || defs[0].Hash == hash {
		t.Fatal("notification policy must affect definition hash", err)
	}
	for _, suffix := range []string{"  notifications: {}\n", "  notifications:\n    mode: inherit\n", "  notifications:\n    mode: disabled\n"} {
		writeDefinition(t, root, "justcd.yaml", validDefinition+suffix)
		defs, err = Discover(root)
		if err != nil || defs[0].Spec.Notifications == nil {
			t.Fatal("inherit/disable", err)
		}
	}
	for _, bad := range []string{strings.Replace(config, "mode: custom", "mode: bogus", 1), strings.Replace(config, `["Production Telegram", "destination-id"]`, `[]`, 1), strings.Replace(config, `["Production Telegram", "destination-id"]`, `[same, same]`, 1), strings.Replace(config, "[sync_failed, unhealthy]", "[]", 1), strings.Replace(config, "[sync_failed, unhealthy]", "[invalid_event]", 1), strings.Replace(config, "minimumSeverity: error", "minimumSeverity: critical", 1), config + "    botToken: NEVER-COMMIT-SECRETS\n", config + "    headers: {Authorization: secret}\n"} {
		writeDefinition(t, root, "justcd.yaml", bad)
		if _, err = Discover(root); err == nil {
			t.Fatal("invalid/secret notification fields accepted")
		}
	}
	writeDefinition(t, root, "justcd.yaml", strings.Replace(config, "    minimumSeverity: error\n", "", 1))
	defs, err = Discover(root)
	if err != nil || defs[0].Spec.Notifications.MinimumSeverity != "info" {
		t.Fatal("default threshold", err)
	}
	writeDefinition(t, root, "justcd.yaml", validDefinition)
	defs, err = Discover(root)
	if err != nil || defs[0].Spec.Notifications != nil {
		t.Fatal("omission must preserve non-Git ownership", err)
	}
}
