package main

import (
	"log"
	"net/http"

	"it-platform/registry/internal/api"
	"it-platform/registry/internal/config"
	"it-platform/registry/internal/github"
	"it-platform/registry/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	gh := github.New(cfg.GitHubBaseURL, cfg.GitHubToken, cfg.GitHubOwner, cfg.GitHubRepo)

	handler := api.NewRouter(&api.Server{Store: st, GitHub: gh, AdminToken: cfg.AdminToken})

	log.Printf("registry-server listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
