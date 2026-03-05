package config

import (
	"fmt"
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
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:       envOr("VEKTOR_LISTEN", ":8659"),
		DataDir:          envOr("VEKTOR_DATA_DIR", "./data"),
		OIDCIssuer:       os.Getenv("VEKTOR_OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("VEKTOR_OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("VEKTOR_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("VEKTOR_OIDC_REDIRECT_URL"),
	}

	if cfg.OIDCIssuer == "" {
		return nil, fmt.Errorf("VEKTOR_OIDC_ISSUER is required")
	}
	if cfg.OIDCClientID == "" {
		return nil, fmt.Errorf("VEKTOR_OIDC_CLIENT_ID is required")
	}
	if cfg.OIDCClientSecret == "" {
		return nil, fmt.Errorf("VEKTOR_OIDC_CLIENT_SECRET is required")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
