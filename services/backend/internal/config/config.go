package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
)

type Config struct {
	Production          bool
	ListenAddress       string
	DatabaseURL         string
	EncryptionKey       []byte
	PublicURL           string
	SessionCookieSecure bool
	SessionLifetime     time.Duration
	// ForceLocalLogin keeps password sign-in available whatever the instance setting says. It is the
	// recovery path if single sign-on breaks after password sign-in was turned off.
	ForceLocalLogin     bool
	ShutdownGracePeriod time.Duration
	FrontendURL         string
	Observability       Observability
	PublishingLimits    publishing.Limits
	PublishingTempDir   string
}

type Observability struct {
	MetricsEnabled   bool
	TracingEnabled   bool
	TraceSampleRatio float64
	OTLPEndpoint     string
}

func Load() (Config, error) {
	var cfg Config
	mode := value("JUSTCD_ENV", "production")
	if mode != "production" && mode != "development" {
		return Config{}, errors.New("JUSTCD_ENV must be production or development")
	}
	cfg.Production = mode == "production"
	cfg.ListenAddress = value("JUSTCD_LISTEN_ADDRESS", ":8080")
	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("JUSTCD_DATABASE_URL"))
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("JUSTCD_DATABASE_URL is required")
	}
	var err error
	cfg.EncryptionKey, err = security.ParseEncryptionKey(os.Getenv("JUSTCD_ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg.PublicURL = strings.TrimRight(value("JUSTCD_PUBLIC_URL", "http://localhost:8080"), "/")
	cfg.FrontendURL = strings.TrimRight(value("JUSTCD_FRONTEND_URL", "http://localhost:3000"), "/")
	secure := value("JUSTCD_COOKIE_SECURE", "true")
	cfg.SessionCookieSecure, err = strconv.ParseBool(secure)
	if err != nil {
		return Config{}, errors.New("JUSTCD_COOKIE_SECURE must be true or false")
	}
	if cfg.Production && (!cfg.SessionCookieSecure || !secureOrigin(cfg.FrontendURL) || !secureOrigin(cfg.PublicURL)) {
		return Config{}, errors.New("production requires HTTPS public/frontend URLs and secure cookies; set JUSTCD_ENV=development only for local development")
	}
	lifetime, err := time.ParseDuration(value("JUSTCD_SESSION_LIFETIME", "12h"))
	if err != nil || lifetime < time.Minute || lifetime > 7*24*time.Hour {
		return Config{}, errors.New("JUSTCD_SESSION_LIFETIME must be between one minute and seven days")
	}
	cfg.SessionLifetime = lifetime
	cfg.ForceLocalLogin, err = boolValue("JUSTCD_FORCE_LOCAL_LOGIN", false)
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownGracePeriod, err = time.ParseDuration(value("JUSTCD_SHUTDOWN_GRACE_PERIOD", "30s"))
	if err != nil || cfg.ShutdownGracePeriod < time.Second || cfg.ShutdownGracePeriod > 10*time.Minute {
		return Config{}, errors.New("JUSTCD_SHUTDOWN_GRACE_PERIOD must be between one second and ten minutes")
	}
	cfg.PublishingLimits.ArchiveBytes, err = publishingSize("JUSTCD_PUBLISH_MAX_ARCHIVE_MIB", publishing.DefaultMaxArchive)
	if err != nil {
		return Config{}, err
	}
	cfg.PublishingLimits.ExpandedBytes, err = publishingSize("JUSTCD_PUBLISH_MAX_EXPANDED_MIB", publishing.DefaultMaxSource)
	if err != nil {
		return Config{}, err
	}
	cfg.PublishingTempDir = strings.TrimSpace(os.Getenv("JUSTCD_PUBLISH_TEMP_DIR"))
	cfg.Observability.MetricsEnabled, err = boolValue("JUSTCD_METRICS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.Observability.TracingEnabled, err = boolValue("JUSTCD_OTEL_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.Observability.TraceSampleRatio = 0.1
	if raw := strings.TrimSpace(os.Getenv("JUSTCD_OTEL_TRACE_SAMPLE_RATIO")); raw != "" {
		cfg.Observability.TraceSampleRatio, err = strconv.ParseFloat(raw, 64)
		if err != nil || cfg.Observability.TraceSampleRatio < 0 || cfg.Observability.TraceSampleRatio > 1 {
			return Config{}, errors.New("JUSTCD_OTEL_TRACE_SAMPLE_RATIO must be between 0 and 1")
		}
	}
	cfg.Observability.OTLPEndpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if cfg.Observability.TracingEnabled && cfg.Observability.OTLPEndpoint == "" {
		return Config{}, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT is required when JUSTCD_OTEL_ENABLED is true")
	}
	return cfg, nil
}

func boolValue(name string, fallback bool) (bool, error) {
	raw := value(name, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New(name + " must be true or false")
	}
	return parsed, nil
}

func value(name, fallback string) string {
	if got := os.Getenv(name); got != "" {
		return got
	}
	return fallback
}

func secureOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
}

func publishingSize(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 10240 {
		return 0, errors.New(name + " must be an integer between 1 and 10240 MiB")
	}
	return n << 20, nil
}
