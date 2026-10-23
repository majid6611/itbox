package api

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/syslog"
)

func TestLogChangesWritesOnlyChanges(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sql, _ := os.ReadFile("../db/migrations/0018_system_log.sql")
	pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	syslog.Default = syslog.NewService(pool)
	defer func() { syslog.Default = nil }()

	last := map[string]string{}
	ok := HealthCheck{Name: "Wiki", Status: "ok", Detail: "Running"}
	down := HealthCheck{Name: "Wiki", Status: "bad", Detail: "wiki: stopped (exit 1)"}
	installing := HealthCheck{Name: "Chat", Status: "warn", Detail: "Still installing"}
	disk := HealthCheck{Name: "Disk space", Status: "warn", Detail: "85% used"}

	logChanges([]HealthCheck{ok, installing}, last)       // first sight, healthy: nothing
	logChanges([]HealthCheck{down, installing}, last)     // went down: 1 error
	logChanges([]HealthCheck{down, installing}, last)     // still down: nothing new
	logChanges([]HealthCheck{ok, installing, disk}, last) // back: 1 info; disk: 1 warn

	entries, total, err := syslog.NewService(pool).List(ctx, syslog.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", total, entries)
	}
	got := entries[2].Level + " " + entries[2].Message
	if !strings.HasPrefix(got, "error Wiki: wiki: stopped") || entries[1].Level != "info" || entries[0].Level != "warn" {
		t.Fatalf("unexpected: %+v", entries)
	}
}
