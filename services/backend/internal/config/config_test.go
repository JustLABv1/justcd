package config

import (
	"strings"
	"testing"
	"time"
)

func TestShutdownGracePeriod(t *testing.T) {
	t.Setenv("JUSTCD_DATABASE_URL", "postgres://test.invalid/justcd")
	t.Setenv("JUSTCD_ENCRYPTION_KEY", strings.Repeat("a", 32))
	t.Setenv("JUSTCD_OTEL_ENABLED", "false")
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 30 * time.Second}, {"1s", time.Second}, {"2m", 2 * time.Minute}, {"10m", 10 * time.Minute},
		{"0s", 0}, {"-1s", 0}, {"500ms", 0}, {"11m", 0}, {"invalid", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("JUSTCD_SHUTDOWN_GRACE_PERIOD", tc.value)
			cfg, err := Load()
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "JUSTCD_SHUTDOWN_GRACE_PERIOD") {
					t.Fatalf("expected grace-period validation error, got %v", err)
				}
			} else if err != nil || cfg.ShutdownGracePeriod != tc.want {
				t.Fatalf("got %v, %v; want %v", cfg.ShutdownGracePeriod, err, tc.want)
			}
		})
	}
}
