package api

import (
	"strings"
	"testing"

	"it-platform/backend/internal/modules"
)

func TestUpdateNoteLoudnessFollowsSeverity(t *testing.T) {
	for sev, want := range map[string]string{"security": "warn", "recommended": "warn", "optional": "info", "": "info"} {
		level, msg := updateNote(modules.UpdateInfo{Name: "Phone System", CurrentVersion: "1.1.4", LatestVersion: "1.1.6", Severity: sev})
		if level != want {
			t.Errorf("severity %q: level %s, want %s", sev, level, want)
		}
		if !strings.Contains(msg, "Phone System") || !strings.Contains(msg, "1.1.4 to 1.1.6") {
			t.Errorf("message should say which module and which versions: %q", msg)
		}
	}
}
