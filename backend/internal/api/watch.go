package api

import (
	"context"
	"time"

	"it-platform/backend/internal/syslog"
)

const healthWatchEvery = time.Minute

// startHealthWatch checks the health list once a minute and writes the
// system log when something changes: a module going down, coming back, disk
// or backups turning bad. Only changes are written — a module that stays
// down is one entry, not one a minute. Nothing else records a crash, since
// health is otherwise only looked at when someone opens the report.
func (s *Server) startHealthWatch(ctx context.Context) {
	if s.Modules == nil {
		return // nothing to watch (tests build a Server without modules)
	}
	go func() {
		last := map[string]string{} // check name -> status
		t := time.NewTicker(healthWatchEvery)
		defer t.Stop()
		for {
			s.watchOnce(ctx, last)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (s *Server) watchOnce(ctx context.Context, last map[string]string) {
	checks, err := s.healthChecks(ctx)
	if err != nil {
		return
	}
	logChanges(checks, last)
}

// logChanges writes the system log for checks whose status differs from the
// last time they were seen.
func logChanges(checks []HealthCheck, last map[string]string) {
	for _, c := range checks {
		prev, seen := last[c.Name]
		last[c.Name] = c.Status
		if seen && prev == c.Status {
			continue
		}
		switch {
		case c.Status == "bad":
			syslog.Error("platform", "%s: %s", c.Name, c.Detail)
		case c.Status == "warn" && c.Detail != "Still installing":
			syslog.Warn("platform", "%s: %s", c.Name, c.Detail)
		case c.Status == "ok" && prev == "bad":
			syslog.Info("platform", "%s is back to normal", c.Name)
		}
	}
}
