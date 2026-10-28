package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	ModulesDir  string
	DataDir     string
	// SnapshotDir is where safety dumps of the database are kept (before
	// upgrades and restores). Must be on persistent storage.
	SnapshotDir   string
	AdminEmail    string
	AdminPassword string
	BaseDomain    string

	NginxConfDir       string
	NginxContainerName string

	GatewayContainerName string

	// ModuleUpdateRepo is the GitHub repository ("owner/name") the Module
	// Store reads new and updated modules from. Empty turns updates off.
	ModuleUpdateRepo string

	// MonitorURL is where licensed installs send their monitoring reports.
	// "off" turns reporting off entirely (supporters running their own
	// service point this at theirs).
	MonitorURL string
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:    getEnv("LISTEN_ADDR", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		ModulesDir:    getEnv("MODULES_DIR", "./modules"),
		DataDir:       getEnv("DATA_DIR", "./data/modules"),
		SnapshotDir:   getEnv("SNAPSHOT_DIR", "./data/snapshots"),
		AdminEmail:    getEnv("ADMIN_EMAIL", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
		BaseDomain:    getEnv("BASE_DOMAIN", "localhost"),

		NginxConfDir:       getEnv("NGINX_CONF_DIR", "./nginx/conf.d"),
		NginxContainerName: getEnv("NGINX_CONTAINER_NAME", "itplatform-nginx"),

		GatewayContainerName: getEnv("GATEWAY_CONTAINER_NAME", "itplatform-internal-gateway"),

		ModuleUpdateRepo: getEnv("MODULE_UPDATE_REPO", "majid6611/itbox"),
		MonitorURL:       getEnv("MONITOR_URL", "https://itboxmonitor.yycit.net"),
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
