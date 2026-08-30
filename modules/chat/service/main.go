package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"it-platform/chat/internal/api"
	"it-platform/chat/internal/dbx"
	"it-platform/chat/internal/hub"
)

func main() {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	pool, err := dbx.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := dbx.Migrate(ctx, pool); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	router := api.NewRouter(&api.Server{DB: pool, Hub: hub.New()})

	log.Printf("chat module listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
