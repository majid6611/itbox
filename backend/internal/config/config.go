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
	SessionSecret string
	BaseDomain    string

	NginxConfDir       string
	NginxContainerName string

	GatewayContainerName string
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:    getEnv("LISTEN_ADDR", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		ModulesDir:    getEnv("MODULES_DIR", "./modules"),
		DataDir:       getEnv("DATA_DIR", "./data/modules"),
		AdminEmail:    getEnv("ADMIN_EMAIL", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
		SessionSecret: getEnv("SESSION_SECRET", ""),
		BaseDomain:    getEnv("BASE_DOMAIN", "localhost"),

		NginxConfDir:       getEnv("NGINX_CONF_DIR", "./nginx/conf.d"),
		NginxContainerName: getEnv("NGINX_CONTAINER_NAME", "itplatform-nginx"),

		GatewayContainerName: getEnv("GATEWAY_CONTAINER_NAME", "itplatform-internal-gateway"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
