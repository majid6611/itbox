// Package employee handles company (LDAP) user sessions for the
// employee portal — deliberately separate from the admin package's own
// admin_users/sessions and cookie, per explicit design: employees never
// share the admin's login, even though both ultimately live in the same
// Postgres database.
package employee

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/directory"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

const sessionTTL = 7 * 24 * time.Hour

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// Login verifies the employee's own LDAP password (a real bind-as-user
// check, not a lookup) and, if valid, issues a session token.
func (s *Service) Login(ctx context.Context, dir *directory.Client, username, password string) (token string, err error) {
	ok, err := dir.VerifyPassword(username, password)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrInvalidCredentials
	}

	token, err = randomToken()
	if err != nil {
		return "", err
	}
	// Sessions nobody will ever use again would otherwise pile up forever.
	_, _ = s.db.Exec(ctx, `DELETE FROM employee_sessions WHERE expires_at < now()`)
	_, err = s.db.Exec(ctx,
		`INSERT INTO employee_sessions (token, username, expires_at) VALUES ($1, $2, $3)`,
		token, username, time.Now().Add(sessionTTL),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// RevokeAll ends every session a user has. Called when an account is
// disabled, deleted or has its password reset: without it, a session cookie
// already in someone's browser would keep working for up to a week after
// they had been cut off. Every module checks this same table, so one
// delete takes access away everywhere at once.
func (s *Service) RevokeAll(ctx context.Context, username string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM employee_sessions WHERE username = $1`, username)
	return err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM employee_sessions WHERE token = $1`, token)
	return err
}

// ValidateSession returns the employee's username for a valid, unexpired
// token.
func (s *Service) ValidateSession(ctx context.Context, token string) (username string, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT username FROM employee_sessions WHERE token = $1 AND expires_at > now()
	`, token).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("session not found or expired")
	}
	return username, err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
