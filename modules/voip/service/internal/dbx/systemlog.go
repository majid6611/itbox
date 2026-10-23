package dbx

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The platform's system log is a table in the shared database (system_log:
// level info|warn|error, source, message). A module reports to it with a
// plain INSERT — there is no other API to call — so it works even when the
// core backend is down, which is when it matters.

var (
	sysMu     sync.Mutex
	sysPool   *pgxpool.Pool
	sysSource string
	sysRecent = map[string]time.Time{}
)

// EnableSystemLog turns on SystemLog for this process; source is the
// module's id, shown in the Logs page.
func EnableSystemLog(pool *pgxpool.Pool, source string) {
	sysMu.Lock()
	defer sysMu.Unlock()
	sysPool, sysSource = pool, source
}

// SystemLog records one line in the platform's system log. It never fails
// the caller, and the same message is written at most once a minute, so a
// failure that repeats on every request can't flood the log.
func SystemLog(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	sysMu.Lock()
	pool, source := sysPool, sysSource
	key := level + "|" + msg
	if t, ok := sysRecent[key]; ok && time.Since(t) < time.Minute {
		sysMu.Unlock()
		return
	}
	if len(sysRecent) > 200 {
		sysRecent = map[string]time.Time{}
	}
	sysRecent[key] = time.Now()
	sysMu.Unlock()
	if pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `INSERT INTO system_log (level, source, message) VALUES ($1, $2, $3)`, level, source, msg); err != nil {
		log.Printf("system log: couldn't record %q: %v", msg, err)
	}
}
