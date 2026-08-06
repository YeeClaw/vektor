package config

import (
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	// Server
	ListenAddr string
	DataDir    string

	// OIDC
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	// Session
	LocalAuth     bool
	SessionSecret string
}

func Load(logger *slog.Logger) (*Config, error) {
	cfg := &Config{
		ListenAddr:       envOr("VEKTOR_LISTEN", ":8659"),
		DataDir:          envOr("VEKTOR_DATA_DIR", "./data"),
		OIDCIssuer:       os.Getenv("VEKTOR_OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("VEKTOR_OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("VEKTOR_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("VEKTOR_OIDC_REDIRECT_URL"),
		LocalAuth:        envOr("VEKTOR_LOCAL_AUTH", "false") == "true",
		SessionSecret:    os.Getenv("VEKTOR_SESSION_SECRET"),
	}

	if !cfg.LocalAuth {
		if cfg.OIDCIssuer == "" {
			return nil, fmt.Errorf("VEKTOR_OIDC_ISSUER is required")
		}
		if cfg.OIDCClientID == "" {
			return nil, fmt.Errorf("VEKTOR_OIDC_CLIENT_ID is required")
		}
		if cfg.OIDCClientSecret == "" {
			return nil, fmt.Errorf("VEKTOR_OIDC_CLIENT_SECRET is required")
		}
	}

	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("VEKTOR_SESSION_SECRET is required")
	} else if len(cfg.SessionSecret) < 32 {
		logger.Warn("session secret is less than 32 characters")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
