package api

import (
	"context"
	"fmt"
	"time"

	"it-platform/backend/internal/modules"
	"it-platform/backend/internal/syslog"
)

const updateWatchEvery = 24 * time.Hour

// startUpdateWatch looks for module updates once a day and writes the system
// log when an installed module has a newer version. The Module Store checks
// too, but only while someone has that page open; an update that fixes a
// security problem shouldn't wait for that. No internet just means no entry.
func (s *Server) startUpdateWatch(ctx context.Context) {
	if s.Modules == nil || s.DB == nil {
		return // tests build a Server without modules
	}
	go func() {
		select { // let the platform finish starting first
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
		}
		for {
			s.watchUpdatesOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(updateWatchEvery):
			}
		}
	}()
}

func (s *Server) watchUpdatesOnce(ctx context.Context) {
	updates, err := s.Modules.CheckForUpdates(ctx)
	if err != nil {
		return
	}
	for _, u := range updates {
		if u.New {
			continue // not installed, nothing to update
		}
		level, msg := updateNote(u)
		// Once per version, even across restarts: the log itself is the memory.
		var seen bool
		if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM system_log WHERE source = 'platform' AND message = $1)`, msg).Scan(&seen); err != nil || seen {
			continue
		}
		if level == "warn" {
			syslog.Warn("platform", "%s", msg)
		} else {
			syslog.Info("platform", "%s", msg)
		}
	}
}

// updateNote is the system-log line for one available update, and how loudly
// to say it: security and recommended updates are warnings.
func updateNote(u modules.UpdateInfo) (level, msg string) {
	level = "info"
	if u.Severity == "security" || u.Severity == "recommended" {
		level = "warn"
	}
	msg = fmt.Sprintf("Update available for %s: %s to %s (%s). Open the Module Store to install it.", u.Name, u.CurrentVersion, u.LatestVersion, u.Severity)
	return level, msg
}
