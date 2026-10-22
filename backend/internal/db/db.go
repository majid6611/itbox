package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migrate applies any .sql files under migrations/ that have not yet been
// recorded in schema_migrations, in filename order. Data safety rules:
//   - each migration runs in its own transaction (all or nothing);
//   - on an existing database, a snapshot is taken first and a failed
//     snapshot stops the upgrade;
//   - a database already upgraded by a NEWER version is never touched by
//     older code (see core_schema_version).
func Migrate(ctx context.Context, pool *pgxpool.Pool, databaseURL, snapshotDir string) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	latest := names[len(names)-1]

	// schema_migrations is shared with the feature modules (wiki, chat, ...),
	// so it can't say which core version wrote the schema; this can.
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS core_schema_version (id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id), version TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create core_schema_version: %w", err)
	}
	var dbVersion string
	_ = pool.QueryRow(ctx, `SELECT version FROM core_schema_version WHERE id`).Scan(&dbVersion)
	if dbVersion > latest {
		return fmt.Errorf("this database was upgraded by a newer version of the platform (schema %s, this version knows up to %s); refusing to start so nothing gets damaged — install the newer version, or restore a backup taken before the upgrade", dbVersion, latest)
	}

	var pending, applied int
	for _, name := range names {
		var done bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name).Scan(&done); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if done {
			applied++
		} else {
			pending++
		}
	}
	if pending > 0 && applied > 0 {
		path, err := Snapshot(ctx, databaseURL, snapshotDir, "before-upgrade")
		if err != nil {
			return fmt.Errorf("not upgrading the database: couldn't take the safety snapshot first: %w", err)
		}
		log.Printf("db: %d pending migration(s); safety snapshot saved to %s", pending, path)
	}

	for _, name := range names {
		var already bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name,
		).Scan(&already)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if already {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	if _, err := pool.Exec(ctx, `INSERT INTO core_schema_version (id, version) VALUES (true, $1) ON CONFLICT (id) DO UPDATE SET version = $1`, latest); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}
