// Package store holds the one thing registry-server needs a database for:
// which client servers are allowed to pull the catalog/bundles, and
// whether that access has been revoked. Deliberately not Postgres — this
// is a handful of rows a single small service owns, so a single SQLite
// file is the whole of "zero ops" rather than standing up a database
// server for a service this small.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrRevoked = errors.New("revoked")

type Client struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// The data directory is created by the container's volume mount
		// in practice, but a fresh local run without one shouldn't fail
		// with an opaque sqlite "unable to open" error.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at TIMESTAMP
		)
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// CreateClient generates a new random key, stores only its hash, and
// returns the plaintext key exactly once — same handling as every other
// generated secret in this platform (see randomSecret in the itbox
// backend), because there's nothing to show an admin a second time if
// they lose it; they'd revoke and issue a new one instead.
func (s *Store) CreateClient(name string) (id int64, plaintextKey string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return 0, "", fmt.Errorf("generate key: %w", err)
	}
	plaintextKey = "reg_" + base64.RawURLEncoding.EncodeToString(raw)

	res, err := s.db.Exec(`INSERT INTO clients (name, key_hash) VALUES (?, ?)`, name, hashKey(plaintextKey))
	if err != nil {
		return 0, "", fmt.Errorf("insert client: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	return id, plaintextKey, nil
}

// Authenticate looks up the client owning this key. Returns ErrNotFound
// for an unrecognized key and ErrRevoked for one that's been disabled —
// callers should treat both as "reject the request," the distinction is
// only for our own logging.
func (s *Store) Authenticate(plaintextKey string) (Client, error) {
	var c Client
	var revokedAt sql.NullTime
	err := s.db.QueryRow(`SELECT id, name, created_at, revoked_at FROM clients WHERE key_hash = ?`, hashKey(plaintextKey)).
		Scan(&c.ID, &c.Name, &c.CreatedAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	if err != nil {
		return Client{}, fmt.Errorf("lookup client: %w", err)
	}
	if revokedAt.Valid {
		c.RevokedAt = &revokedAt.Time
		return c, ErrRevoked
	}
	return c, nil
}

func (s *Store) ListClients() ([]Client, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, revoked_at FROM clients ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	defer rows.Close()

	var out []Client
	for rows.Next() {
		var c Client
		var revokedAt sql.NullTime
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan client: %w", err)
		}
		if revokedAt.Valid {
			c.RevokedAt = &revokedAt.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) RevokeClient(id int64) error {
	res, err := s.db.Exec(`UPDATE clients SET revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke client: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
