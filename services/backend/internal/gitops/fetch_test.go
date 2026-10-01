package gitops

import "testing"

func TestValidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https", url: "https://github.com/justlab/justcd.git", want: true},
		{name: "ssh uri", url: "ssh://git@git.example.com/team/app.git", want: true},
		{name: "ssh uri password", url: "ssh://git:secret@git.example.com/team/app.git", want: false},
		{name: "scp syntax", url: "git@git.example.com:team/app.git", want: true},
		{name: "embedded password", url: "https://user:password@example.com/team/app.git", want: false},
		{name: "query credential", url: "https://example.com/team/app.git?token=secret", want: false},
		{name: "unsupported scheme", url: "file:///etc/passwd", want: false},
		{name: "missing host", url: "https:///team/app.git", want: false},
		{name: "malformed scp", url: "git@:team/app.git", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validURL(test.url); got != test.want {
				t.Errorf("validURL(%q) = %t, want %t", test.url, got, test.want)
			}
		})
	}
}

func TestGitEnvironmentPreservesCATrustAndRejectsTLSBypass(t *testing.T) {
	env := cleanGitEnvironment([]string{"GIT_SSL_CAINFO=/custom/ca.crt", "GIT_SSL_CAPATH=/etc/ssl/certs", "GIT_SSL_NO_VERIFY=true"}, "/isolated")
	seen := map[string]bool{}
	for _, entry := range env {
		seen[entry] = true
	}
	if !seen["GIT_SSL_CAINFO=/custom/ca.crt"] || !seen["GIT_SSL_CAPATH=/etc/ssl/certs"] {
		t.Fatalf("CA configuration lost: %v", env)
	}
	if seen["GIT_SSL_NO_VERIFY=true"] {
		t.Fatal("TLS bypass leaked into Git environment")
	}
}
