package main

import (
	"context"
	"log"
	"net/http"

	"it-platform/backend/internal/api"
	"it-platform/backend/internal/audit"
	"it-platform/backend/internal/auth"
	"it-platform/backend/internal/config"
	"it-platform/backend/internal/db"
	"it-platform/backend/internal/docker"
	"it-platform/backend/internal/employee"
	"it-platform/backend/internal/modules"
	"it-platform/backend/internal/proxy"
	"it-platform/backend/internal/registryclient"
	"it-platform/backend/internal/syslog"
	"it-platform/backend/internal/version"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, cfg.DatabaseURL, cfg.SnapshotDir); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	authService := auth.NewService(pool)
	if err := authService.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("seed admin: %v", err)
	}
	employeeService := employee.NewService(pool)

	registry, err := modules.NewRegistry(cfg.ModulesDir)
	if err != nil {
		log.Fatalf("load module registry: %v", err)
	}

	dockerClient := docker.NewClient()
	proxyManager := proxy.NewManager(dockerClient, cfg.NginxConfDir, cfg.NginxContainerName)
	regClient := registryclient.New(cfg.ModuleUpdateRepo)
	moduleManager, err := modules.NewManager(ctx, registry, dockerClient, proxyManager, pool, cfg.DataDir, cfg.BaseDomain, cfg.DatabaseURL, regClient)
	if err != nil {
		log.Fatalf("module manager: %v", err)
	}

	moduleManager.SnapshotDir = cfg.SnapshotDir
	syslog.Default = syslog.NewService(pool)
	syslog.Info("platform", "Platform started (version %s)", version.Version)
	srv := &api.Server{
		Auth:                 authService,
		Employee:             employeeService,
		Docker:               dockerClient,
		Modules:              moduleManager,
		Registry:             registry,
		DB:                   pool,
		Audit:                audit.NewService(pool),
		SystemLog:            syslog.Default,
		DatabaseURL:          cfg.DatabaseURL,
		SnapshotDir:          cfg.SnapshotDir,
		GatewayContainerName: cfg.GatewayContainerName,
	}
	go srv.SyncGatewayRoutesOnStart()
	router := api.NewRouter(srv)

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
