package config

import (
	"strings"
	"testing"
	"time"
)

func TestShutdownGracePeriod(t *testing.T) {
	t.Setenv("JUSTCD_ENV", "development")
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

func TestPublishingLimits(t *testing.T) {
	t.Setenv("JUSTCD_ENV", "development")
	t.Setenv("JUSTCD_DATABASE_URL", "postgres://test.invalid/justcd")
	t.Setenv("JUSTCD_ENCRYPTION_KEY", strings.Repeat("a", 32))
	t.Setenv("JUSTCD_OTEL_ENABLED", "false")
	t.Setenv("JUSTCD_PUBLISH_MAX_ARCHIVE_MIB", "")
	t.Setenv("JUSTCD_PUBLISH_MAX_EXPANDED_MIB", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublishingLimits.ArchiveBytes != 100<<20 || cfg.PublishingLimits.ExpandedBytes != 500<<20 {
		t.Fatal("unexpected publishing defaults")
	}
	for _, key := range []string{"JUSTCD_PUBLISH_MAX_ARCHIVE_MIB", "JUSTCD_PUBLISH_MAX_EXPANDED_MIB"} {
		for _, value := range []string{"0", "-1", "1.5", "10241", "9223372036854775807", "invalid"} {
			t.Run(key+value, func(t *testing.T) {
				t.Setenv(key, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
					t.Fatal("invalid publishing limit accepted", err)
				}
			})
		}
	}
	t.Setenv("JUSTCD_PUBLISH_MAX_ARCHIVE_MIB", "250")
	t.Setenv("JUSTCD_PUBLISH_MAX_EXPANDED_MIB", "1024")
	t.Setenv("JUSTCD_PUBLISH_TEMP_DIR", "/tmp/publishing")
	cfg, err = Load()
	if err != nil || cfg.PublishingLimits.ArchiveBytes != 250<<20 || cfg.PublishingLimits.ExpandedBytes != 1024<<20 || cfg.PublishingTempDir != "/tmp/publishing" {
		t.Fatal("configured limits not applied", err)
	}
}
