package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestCommandFailureClassifiesGitTransportErrors(t *testing.T) {
	for _, test := range []struct {
		stderr    string
		retryable bool
	}{
		{"The requested URL returned error: 408", true},
		{"The requested URL returned error: 429", true},
		{"The requested URL returned error: 500", true},
		{"The requested URL returned error: 502", true},
		{"The requested URL returned error: 503", true},
		{"The requested URL returned error: 504", true},
		{"RPC failed; HTTP 503", true},
		{"RPC failed; curl 92 HTTP/2 stream 3 was not closed cleanly", true},
		{"RPC failed; curl 18 transfer closed with outstanding read data remaining", true},
		{"RPC failed; curl 28 Operation timed out", true},
		{"RPC failed; curl 56 Failure when receiving data from the peer", true},
		{"ssh: Could not resolve hostname git.example.com", true},
		{"Connection closed by remote host", true},
		{"Failed to connect to git.example.com port 443", true},
		{"fatal: early EOF", true},
		{"The requested URL returned error: 401", false},
		{"The requested URL returned error: 403", false},
		{"The requested URL returned error: 404", false},
		{"Authentication failed", false},
		{"Permission denied (publickey)", false},
		{"Host key verification failed", false},
		{"SSL certificate problem: certificate has expired", false},
		{"couldn't find remote ref missing", false},
	} {
		t.Run(test.stderr, func(t *testing.T) {
			err := commandFailure(context.Background(), "clone Git source", &exec.ExitError{Stderr: []byte(test.stderr + " PRIVATE-TOKEN")})
			var fetch *FetchError
			if !errors.As(err, &fetch) || fetch.Retryable() != test.retryable {
				t.Fatalf("unexpected classification: %v, %#v", err, fetch)
			}
			if strings.Contains(err.Error(), "PRIVATE-TOKEN") {
				t.Fatal("Git stderr leaked into error")
			}
		})
	}
}

func TestCommandFailurePreservesTimeoutAndCancellation(t *testing.T) {
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{deadline, cancelled} {
		if err := commandFailure(ctx, "check out requested Git revision", errors.New("signal: killed")); !errors.Is(err, ctx.Err()) {
			t.Fatalf("lost context error: %v", err)
		}
	}
}

func TestFetchPreservesRetryClassificationAtEveryCheckoutStage(t *testing.T) {
	for _, stage := range []string{"clone", "config", "fetch", "rev-parse", "checkout"} {
		t.Run(stage, func(t *testing.T) {
			bin := t.TempDir()
			// A partial clone may fetch missing blobs during checkout as well.
			script := "#!/bin/sh\nif [ \"$1\" = '-C' ]; then shift 2; fi\n" +
				"if [ \"$1\" = " + shellQuote(stage) + " ]; then echo 'The requested URL returned error: 500 PRIVATE-TOKEN' >&2; exit 128; fi\n" +
				"if [ \"$1\" = 'rev-parse' ]; then echo 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; fi\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			checkout, err := Fetch(context.Background(), nil, nil, store.GitSource{RepositoryURL: "https://git.example.com/team/app.git"}, "main")
			defer checkout.Close()
			var fetch *FetchError
			if !errors.As(err, &fetch) || !fetch.Retryable() {
				t.Fatalf("%s failure lost retry classification: %v", stage, err)
			}
			if strings.Contains(err.Error(), "PRIVATE-TOKEN") {
				t.Fatal("Git stderr leaked into error")
			}
		})
	}
}

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
