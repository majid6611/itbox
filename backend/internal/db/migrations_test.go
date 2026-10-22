package db

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// destructive matches statements that can lose or rewrite existing data.
// Dropping a constraint or index doesn't, so those are deliberately absent.
var destructive = regexp.MustCompile(`(?i)\b(DROP\s+(TABLE|COLUMN|SCHEMA|DATABASE)|TRUNCATE|DELETE\s+FROM|ALTER\s+COLUMN\s+\S+\s+(SET\s+DATA\s+)?TYPE)\b`)

// TestMigrationsDontDestroyData enforces the upgrade rule: customer data
// is never deleted by a version change. A migration that really must do
// so has to say why, with a `-- allow-destructive: <reason>` line, which
// turns the decision into something a reviewer sees.
func TestMigrationsDontDestroyData(t *testing.T) {
	files, _ := filepath.Glob("migrations/*.sql")
	modFiles, _ := filepath.Glob("../../../modules/*/service/internal/dbx/migrations/*.sql")
	files = append(files, modFiles...)
	if len(files) == 0 {
		t.Fatal("found no migrations to check")
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var code []string
		allowed := false
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "-- allow-destructive:") {
				allowed = true
			}
			if !strings.HasPrefix(trimmed, "--") {
				code = append(code, line)
			}
		}
		if m := destructive.FindString(strings.Join(code, "\n")); m != "" && !allowed {
			t.Errorf("%s contains %q; add `-- allow-destructive: <why no data is lost>` if it is safe", f, m)
		}
	}
}
