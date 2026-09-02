package main

import (
	"context"
	"log"
	"net/http"

	"it-platform/backend/internal/api"
	"it-platform/backend/internal/auth"
	"it-platform/backend/internal/config"
	"it-platform/backend/internal/db"
	"it-platform/backend/internal/docker"
	"it-platform/backend/internal/employee"
	"it-platform/backend/internal/modules"
	"it-platform/backend/internal/proxy"
	"it-platform/backend/internal/registryclient"
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

	if err := db.Migrate(ctx, pool); err != nil {
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
	regClient := registryclient.New(cfg.ModuleRegistryURL, cfg.ModuleRegistryKey)
	moduleManager, err := modules.NewManager(ctx, registry, dockerClient, proxyManager, pool, cfg.DataDir, cfg.BaseDomain, cfg.DatabaseURL, regClient)
	if err != nil {
		log.Fatalf("module manager: %v", err)
	}

	router := api.NewRouter(&api.Server{
		Auth:                 authService,
		Employee:             employeeService,
		Docker:               dockerClient,
		Modules:              moduleManager,
		Registry:             registry,
		DB:                   pool,
		GatewayContainerName: cfg.GatewayContainerName,
	})

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
