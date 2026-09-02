package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr    string
	DatabaseURL   string
	ModulesDir    string
	DataDir       string
	AdminEmail    string
	AdminPassword string
	BaseDomain    string

	NginxConfDir       string
	NginxContainerName string

	GatewayContainerName string

	// ModuleRegistryURL/Key point this deployment at registry-server (see
	// the repo's registry/ directory) — the vendor-run service that
	// discovers and delivers new/updated modules. Both are optional and
	// empty by default: a deployment with no key configured just never
	// sees update/new-module badges, rather than failing to start.
	ModuleRegistryURL string
	ModuleRegistryKey string
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:    getEnv("LISTEN_ADDR", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		ModulesDir:    getEnv("MODULES_DIR", "./modules"),
		DataDir:       getEnv("DATA_DIR", "./data/modules"),
		AdminEmail:    getEnv("ADMIN_EMAIL", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
		BaseDomain:    getEnv("BASE_DOMAIN", "localhost"),

		NginxConfDir:       getEnv("NGINX_CONF_DIR", "./nginx/conf.d"),
		NginxContainerName: getEnv("NGINX_CONTAINER_NAME", "itplatform-nginx"),

		GatewayContainerName: getEnv("GATEWAY_CONTAINER_NAME", "itplatform-internal-gateway"),

		ModuleRegistryURL: getEnv("MODULE_REGISTRY_URL", ""),
		ModuleRegistryKey: getEnv("MODULE_REGISTRY_KEY", ""),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
