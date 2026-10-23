package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/audit"
	"it-platform/backend/internal/auth"
	"it-platform/backend/internal/employee"
	"it-platform/backend/internal/syslog"
)

// Real requests through the real router against a real Postgres:
// TEST_DATABASE_URL=postgres://... go test ./internal/api -run Audit
func TestAuditThroughTheRouter(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../db/migrations/*.sql")
	for _, f := range files {
		sql, _ := os.ReadFile(f)
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	authSvc := auth.NewService(pool)
	if err := authSvc.EnsureAdmin(ctx, "boss@example.com", "correct-horse"); err != nil {
		t.Fatal(err)
	}
	srv := &Server{Auth: authSvc, Employee: employee.NewService(pool), DB: pool, Audit: audit.NewService(pool), SystemLog: syslog.NewService(pool)}
	syslog.Default = srv.SystemLog
	defer func() { syslog.Default = nil }()
	router := NewRouter(srv)

	do := func(method, path, body, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Real-IP", "10.0.0.7")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// A wrong password, then the right one.
	if rec := do("POST", "/api/auth/login", `{"email":"boss@example.com","password":"nope"}`, ""); rec.Code != 401 {
		t.Fatalf("bad login: %d", rec.Code)
	}
	rec := do("POST", "/api/auth/login", `{"email":"boss@example.com","password":"correct-horse"}`, "")
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	cookie := strings.SplitN(rec.Header().Get("Set-Cookie"), ";", 2)[0]

	// A read that must NOT be recorded, a change without a session (refused,
	// but still recorded with its target), and a signed-in change.
	do("GET", "/api/auth/me", "", cookie)
	do("POST", "/api/modules/wiki/disable", `{}`, "")
	do("POST", "/api/auth/logout", `{}`, cookie)
	rec = do("POST", "/api/auth/login", `{"email":"boss@example.com","password":"correct-horse"}`, "")
	cookie = strings.SplitN(rec.Header().Get("Set-Cookie"), ";", 2)[0]

	rec = do("GET", "/api/audit", "", cookie)
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`"action":"login-failed"`, `"action":"login"`, `"action":"disable-module"`, `"target":"wiki"`, `"status":401`, `"action":"logout"`, `"ip":"10.0.0.7"`, `"actor":"boss@example.com"`, `"total":5`} {
		if !strings.Contains(body, want) {
			t.Errorf("list is missing %s\n%s", want, body)
		}
	}
	if strings.Contains(body, "correct-horse") || strings.Contains(body, `"action":"me"`) {
		t.Errorf("list has something it must not: %s", body)
	}
	if rec := do("GET", "/api/audit", "", ""); rec.Code != 401 {
		t.Errorf("audit log without a session: %d", rec.Code)
	}

	if rec := do("GET", "/api/audit/verify", "", cookie); !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("verify: %s", rec.Body)
	}

	rec = do("GET", "/api/audit/export?action=login", "", cookie)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(rec.Body.String(), "time,who,kind,action") || !strings.Contains(rec.Body.String(), "boss@example.com") {
		t.Errorf("export: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	// The export itself is recorded.
	if !strings.Contains(do("GET", "/api/audit?action=export", "", cookie).Body.String(), `"action":"export-audit-log"`) {
		t.Error("export was not recorded")
	}

	// System log: a platform event and a module's own insert both show up.
	syslog.Warn("platform", "disk 85%% used")
	pool.Exec(ctx, `INSERT INTO system_log (level, source, message) VALUES ('error', 'chat', 'cannot reach storage')`)
	body = do("GET", "/api/system-log?level=warn", "", cookie).Body.String()
	for _, want := range []string{"disk 85% used", "cannot reach storage", `"sources":["chat","platform"]`, `"total":2`} {
		if !strings.Contains(body, want) {
			t.Errorf("system log is missing %s\n%s", want, body)
		}
	}
	if rec := do("GET", "/api/system-log", "", ""); rec.Code != 401 {
		t.Errorf("system log without a session: %d", rec.Code)
	}
	rec = do("GET", "/api/system-log/export?source=chat", "", cookie)
	if !strings.Contains(rec.Body.String(), "time,level,source,message") || !strings.Contains(rec.Body.String(), "cannot reach storage") || strings.Contains(rec.Body.String(), "disk 85") {
		t.Errorf("system log export: %s", rec.Body)
	}
	_ = http.StatusOK
}
