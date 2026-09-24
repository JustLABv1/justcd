package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
)

type Config struct {
	ListenAddress          string
	DatabaseURL            string
	EncryptionKey          []byte
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
	PublicURL              string
	SessionCookieSecure    bool
	SessionLifetime        time.Duration
	FrontendURL            string
}

func Load() (Config, error) {
	var cfg Config
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
	cfg.BootstrapAdminEmail = strings.TrimSpace(os.Getenv("JUSTCD_BOOTSTRAP_ADMIN_EMAIL"))
	cfg.BootstrapAdminPassword = os.Getenv("JUSTCD_BOOTSTRAP_ADMIN_PASSWORD")
	cfg.PublicURL = strings.TrimRight(value("JUSTCD_PUBLIC_URL", "http://localhost:8080"), "/")
	cfg.FrontendURL = strings.TrimRight(value("JUSTCD_FRONTEND_URL", "http://localhost:3000"), "/")
	secure := value("JUSTCD_COOKIE_SECURE", "true")
	cfg.SessionCookieSecure, err = strconv.ParseBool(secure)
	if err != nil {
		return Config{}, errors.New("JUSTCD_COOKIE_SECURE must be true or false")
	}
	lifetime, err := time.ParseDuration(value("JUSTCD_SESSION_LIFETIME", "12h"))
	if err != nil || lifetime < time.Minute || lifetime > 7*24*time.Hour {
		return Config{}, errors.New("JUSTCD_SESSION_LIFETIME must be between one minute and seven days")
	}
	cfg.SessionLifetime = lifetime
	return cfg, nil
}

func value(name, fallback string) string {
	if got := os.Getenv(name); got != "" {
		return got
	}
	return fallback
}
