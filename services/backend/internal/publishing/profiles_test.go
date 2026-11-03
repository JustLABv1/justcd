package publishing

import "testing"

func profileSettings() ProfileSettings {
	return ProfileSettings{TemplateID: "shiny", GitLabURL: "https://gitlab.example.com", Group: "analytics/apps", ClusterID: "cluster", Namespace: "analytics", BaseImage: "registry.example.com/base/shiny:1", BuilderImage: "registry.example.com/base/builder:1", RegistryPrefix: "registry.example.com/analytics", HostSuffix: "apps.example.com", SecretMode: "variables", SyncPolicy: "auto-safe"}
}
func TestProfileResolvesApprovedInfrastructure(t *testing.T) {
	s := profileSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	c, err := s.Resolve("regional-report", "src/report")
	if err != nil {
		t.Fatal(err)
	}
	if c.RegistryImage != "registry.example.com/analytics/regional-report" || c.Host != "regional-report.apps.example.com" || c.EntryPoint != "src/report" || c.Namespace != "analytics" || c.Runtime != "shiny" {
		t.Fatal("profile settings not applied", c)
	}
	s.TemplateID = "streamlit"
	c, err = s.Resolve("report", "")
	if err != nil || c.EntryPoint != "app.py" || c.Runtime != "streamlit" {
		t.Fatal("template defaults not applied", err)
	}
	for _, name := range []string{"../report", "Report", "report/escape", "report;echo"} {
		if _, err = s.Resolve(name, ""); err == nil {
			t.Fatal("unsafe application name accepted", name)
		}
	}
}
func TestProfileRejectsInvalidDestinations(t *testing.T) {
	for _, change := range []func(*ProfileSettings){func(s *ProfileSettings) { s.TemplateID = "unknown" }, func(s *ProfileSettings) { s.Group = "../apps" }, func(s *ProfileSettings) { s.HostSuffix = "https://apps.example.com" }, func(s *ProfileSettings) { s.RegistryPrefix = "registry.example.com" }, func(s *ProfileSettings) { s.RegistryPrefix = "registry.example.com/apps:latest" }, func(s *ProfileSettings) { s.RegistryPrefix = "registry.example.com/a/../apps" }, func(s *ProfileSettings) { s.SyncPolicy = "auto" }, func(s *ProfileSettings) { s.ClusterID = "" }} {
		s := profileSettings()
		change(&s)
		if err := s.Validate(); err == nil {
			t.Fatal("invalid publishing profile accepted", s)
		}
	}
}
