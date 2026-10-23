package audit

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test ./internal/audit
func testService(t *testing.T) (*Service, *pgxpool.Pool) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	base, _ := os.ReadFile("../db/migrations/0001_init.sql")
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(base)); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../db/migrations/0017_audit_log.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	return NewService(pool), pool
}

func TestChainVerifyAndTamper(t *testing.T) {
	s, pool := testService(t)
	ctx := context.Background()
	for _, a := range []string{"login", "install-module", "delete-user", "update-module"} {
		s.Record(Entry{Actor: "admin@x.com", ActorKind: "admin", Action: a, Target: "t", IP: "1.2.3.4", Status: 200, Detail: "d"})
	}
	res, err := s.Verify(ctx)
	if err != nil || !res.OK || res.Checked != 4 {
		t.Fatalf("fresh chain: %+v %v", res, err)
	}

	// The database refuses edits and early deletes outright.
	if _, err := pool.Exec(ctx, `UPDATE audit_log SET actor='x' WHERE id=2`); err == nil || !strings.Contains(err.Error(), "cannot be edited") {
		t.Fatalf("update should be refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE id=2`); err == nil || !strings.Contains(err.Error(), "one year") {
		t.Fatalf("delete should be refused: %v", err)
	}

	// Someone with superuser rights who turns the guard off is still caught.
	pool.Exec(ctx, `ALTER TABLE audit_log DISABLE TRIGGER audit_log_guard`)
	pool.Exec(ctx, `UPDATE audit_log SET action='nothing-happened' WHERE id=2`)
	res, _ = s.Verify(ctx)
	if res.OK || res.BrokeAt != 2 {
		t.Fatalf("edit not detected: %+v", res)
	}
	pool.Exec(ctx, `UPDATE audit_log SET action='install-module' WHERE id=2`)
	if res, _ = s.Verify(ctx); !res.OK {
		t.Fatalf("restored chain should verify: %+v", res)
	}
	pool.Exec(ctx, `DELETE FROM audit_log WHERE id=3`)
	res, _ = s.Verify(ctx)
	if res.OK || !res.Missing || res.BrokeAt != 4 {
		t.Fatalf("removed row not detected: %+v", res)
	}
}

func TestPruneKeepsRecentAndChainSurvives(t *testing.T) {
	s, pool := testService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		s.Record(Entry{Action: "a"})
	}
	pool.Exec(ctx, `ALTER TABLE audit_log DISABLE TRIGGER audit_log_guard`)
	pool.Exec(ctx, `UPDATE audit_log SET at = at - interval '2 years' WHERE id = 1`)
	pool.Exec(ctx, `ALTER TABLE audit_log ENABLE TRIGGER audit_log_guard`)
	// Row 1 was edited above, so re-seal it the honest way is impossible; the
	// point here is only that pruning removes what is past retention.
	if err := s.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
	if n != 2 {
		t.Fatalf("want 2 rows left, got %d", n)
	}
	if res, _ := s.Verify(ctx); !res.OK || res.Checked != 2 {
		t.Fatalf("chain after prune: %+v", res)
	}
}

func TestConcurrentWritersKeepChain(t *testing.T) {
	s, _ := testService(t)
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() { s.Record(Entry{Action: "x"}); done <- struct{}{} }()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	if res, err := s.Verify(context.Background()); err != nil || !res.OK || res.Checked != 20 {
		t.Fatalf("%+v %v", res, err)
	}
}
