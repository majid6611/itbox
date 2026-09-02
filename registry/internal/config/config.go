package config

import (
	"fmt"
	"os"
)

// Config holds registry-server's own settings. This service is
// infrastructure we run, not something a client deploys — its env is
// entirely separate from the itbox platform's own backend config.
type Config struct {
	ListenAddr string
	DBPath     string

	// GitHubToken authenticates every request this service makes to
	// GitHub on clients' behalf — clients never see it or talk to GitHub
	// directly, only to us.
	GitHubToken   string
	GitHubOwner   string
	GitHubRepo    string
	GitHubBaseURL string // overridable for local testing against a stand-in server

	// AdminToken authenticates the client-key management endpoints
	// (/v1/admin/*) — separate from any per-client key, since issuing or
	// revoking a client's access is an operation only we should be able
	// to do.
	AdminToken string
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:    getEnv("LISTEN_ADDR", ":8090"),
		DBPath:        getEnv("DB_PATH", "./data/registry.db"),
		GitHubToken:   getEnv("GITHUB_TOKEN", ""),
		GitHubOwner:   getEnv("GITHUB_OWNER", ""),
		GitHubRepo:    getEnv("GITHUB_REPO", ""),
		GitHubBaseURL: getEnv("GITHUB_BASE_URL", "https://api.github.com"),
		AdminToken:    getEnv("ADMIN_TOKEN", ""),
	}
	if cfg.GitHubToken == "" || cfg.GitHubOwner == "" || cfg.GitHubRepo == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN, GITHUB_OWNER, and GITHUB_REPO are required")
	}
	if cfg.AdminToken == "" {
		return nil, fmt.Errorf("ADMIN_TOKEN is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
