package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"it-platform/voip/internal/ami"
	"it-platform/voip/internal/api"
	"it-platform/voip/internal/dbx"
)

func main() {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	amiSecret := os.Getenv("AMI_SECRET")
	if amiSecret == "" {
		log.Fatal("AMI_SECRET is required")
	}
	amiAddr := os.Getenv("AMI_ADDR")
	if amiAddr == "" {
		// Asterisk runs on the host's network, listening for AMI on the
		// host's own edge-network address — which, from inside this
		// container (attached to edge only), is just its default gateway.
		gw, err := defaultGateway()
		if err != nil {
			log.Fatalf("find asterisk manager address: %v", err)
		}
		amiAddr = net.JoinHostPort(gw, "5038")
	}
	listenAddr := envOr("LISTEN_ADDR", ":8080")

	pool, err := dbx.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := dbx.Migrate(ctx, pool); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	s := &api.Server{
		DB:        pool,
		AMI:       &ami.Client{Addr: amiAddr, Username: "itp", Secret: amiSecret},
		ConfigDir: envOr("ASTERISK_CONFIG_DIR", "/asterisk/itp"),
		SoundsDir: envOr("ASTERISK_SOUNDS_DIR", "/sounds"),
		CDRFile:   envOr("ASTERISK_CDR_FILE", "/asterisk/cdr/Master.csv"),
	}
	// Asterisk runs as another user and records the greeting into this
	// folder when someone dials *99, so it must be open to it.
	if err := os.MkdirAll(s.SoundsDir, 0o777); err == nil {
		_ = os.Chmod(s.SoundsDir, 0o777)
		// A greeting saved by an earlier version may not be writable by it.
		_ = os.Chmod(filepath.Join(s.SoundsDir, "greeting.wav"), 0o666)
	}
	// Write Asterisk's config from the database on every start, so the
	// generated files always match what's stored — including after an
	// update that changes how they're generated.
	if err := s.Apply(ctx); err != nil {
		log.Fatalf("write asterisk config: %v", err)
	}

	log.Printf("voip module listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, api.NewRouter(s)); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// defaultGateway reads the container's default route from /proc/net/route
// (destination 00000000; gateway is little-endian hex).
func defaultGateway() (string, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		raw, err := hex.DecodeString(f[2])
		if err != nil || len(raw) != 4 {
			continue
		}
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]).String(), nil
	}
	return "", fmt.Errorf("no default route")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
