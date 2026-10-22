package db

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keepSnapshots is how many safety snapshots stay on disk; older ones are
// pruned so they can't quietly fill the server.
const keepSnapshots = 5

// Snapshot writes a full pg_dump of the database into dir before something
// that could lose data (a schema migration, replacing the database in a
// restore) and returns the file's path. reason becomes part of the file
// name so an admin can tell them apart. Callers must treat a failure as
// "don't proceed" — the point is that there is always a way back.
func Snapshot(ctx context.Context, databaseURL, dir, reason string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.dump", time.Now().UTC().Format("20060102-150405"), reason))
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "--no-owner", "-f", path, databaseURL)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	pruneSnapshots(dir)
	return path, nil
}

func pruneSnapshots(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.dump"))
	sort.Strings(files) // timestamp-prefixed, so oldest first
	for len(files) > keepSnapshots {
		os.Remove(files[0])
		files = files[1:]
	}
}
