package dbx

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs the platform's system_log table: TEST_DATABASE_URL=postgres://...
func TestSystemLogWritesAndThrottles(t *testing.T) {
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
	pool.Exec(ctx, "DROP TABLE IF EXISTS system_log")
	if _, err := pool.Exec(ctx, `CREATE TABLE system_log (id BIGSERIAL PRIMARY KEY, at TIMESTAMPTZ NOT NULL DEFAULT now(), level TEXT NOT NULL DEFAULT 'info', source TEXT NOT NULL, message TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	EnableSystemLog(pool, "wiki")
	for i := 0; i < 5; i++ {
		SystemLog("error", "storage unreachable: %s", "dial tcp") // repeats: written once
	}
	SystemLog("info", "Started")
	var n int
	var source string
	pool.QueryRow(ctx, `SELECT count(*), min(source) FROM system_log`).Scan(&n, &source)
	if n != 2 || source != "wiki" {
		t.Fatalf("want 2 rows from wiki, got %d from %q", n, source)
	}
}
