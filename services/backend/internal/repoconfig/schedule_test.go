package repoconfig

import (
	"strings"
	"testing"
)

func TestDeploymentScheduleManifest(t *testing.T) {
	root := t.TempDir()
	config := validDefinition + `  deploymentSchedule:
    windows:
      - timeZone: Europe/Berlin
        days: [1, 2, 3, 4, 5]
        start: "09:00"
        end: "17:00"
    freezes:
      - reason: Backup
        schedule:
          timeZone: UTC
          days: [0]
          start: "01:00"
          end: "03:00"
      - reason: Release freeze
        start: "2026-12-24T00:00:00+01:00"
        end: "2027-01-02T00:00:00+01:00"
`
	writeDefinition(t, root, "justcd.yaml", config)
	defs, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p := defs[0].Spec.DeploymentSchedule
	if len(p.Windows) != 1 || len(p.Freezes) != 2 || p.Windows[0].TimeZone != "Europe/Berlin" {
		t.Fatalf("lost schedule %+v", p)
	}
	hash := defs[0].Hash
	writeDefinition(t, root, "justcd.yaml", strings.Replace(config, "09:00", "10:00", 1))
	defs, err = Discover(root)
	if err != nil || defs[0].Hash == hash {
		t.Fatalf("policy must affect definition hash: %v", err)
	}
	for _, bad := range []string{strings.Replace(config, "Europe/Berlin", "Bad/Zone", 1), strings.Replace(config, "[1, 2, 3, 4, 5]", "[7]", 1), strings.Replace(config, "reason: Backup", "reason: ''", 1), strings.Replace(config, "09:00", "25:00", 1), strings.Replace(config, "windows:", "unknownWindows:", 1)} {
		writeDefinition(t, root, "justcd.yaml", bad)
		if _, err := Discover(root); err == nil {
			t.Fatal("invalid schedule accepted")
		}
	}
}
