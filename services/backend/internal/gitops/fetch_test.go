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
