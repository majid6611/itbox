package syslog

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListFilterPruneAndModuleWrites(t *testing.T) {
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
	s := NewService(pool)
	Default = s
	defer func() { Default = nil }()

	Info("platform", "started")
	Warn("platform", "disk %d%% used", 85)
	Error("wiki", "database unreachable")
	// A module writes straight to the table with plain SQL — the contract.
	if _, err := pool.Exec(ctx, `INSERT INTO system_log (level, source, message) VALUES ('error', 'chat', 'x')`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		f    Filter
		want int
	}{
		{Filter{}, 4},
		{Filter{Level: "warn"}, 3},
		{Filter{Level: "error"}, 2},
		{Filter{Source: "wiki"}, 1},
		{Filter{Search: "DISK"}, 1},
	}
	for _, c := range cases {
		if _, total, err := s.List(ctx, c.f); err != nil || total != c.want {
			t.Errorf("%+v: total %d (want %d) %v", c.f, total, c.want, err)
		}
	}
	if src, _ := s.Sources(ctx); len(src) != 3 {
		t.Errorf("sources: %v", src)
	}

	pool.Exec(ctx, `UPDATE system_log SET at = now() - interval '91 days' WHERE source = 'wiki'`)
	if err := s.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.List(ctx, Filter{}); total != 3 {
		t.Errorf("after prune: %d", total)
	}
	var nilSvc *Service
	nilSvc.write("info", "x", "no database", nil) // must not panic
}
