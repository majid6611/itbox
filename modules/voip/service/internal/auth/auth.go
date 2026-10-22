// Package auth validates sessions the core platform already issued —
// admin sessions (itp_session) and employee sessions (itp_employee_session)
// — by reading the same tables core writes them into. Core is the identity
// authority (LDAP bind, admin login, session issuance all live there); this
// module never issues or invalidates a session itself, only checks one.
package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidSession = errors.New("session not found or expired")

// ValidateAdminSession returns the admin's email for a valid, unexpired
// itp_session token.
func ValidateAdminSession(ctx context.Context, db *pgxpool.Pool, token string) (email string, err error) {
	if token == "" {
		return "", ErrInvalidSession
	}
	err = db.QueryRow(ctx, `
		SELECT admin_users.email FROM sessions
		JOIN admin_users ON admin_users.id = sessions.admin_user_id
		WHERE sessions.token = $1 AND sessions.expires_at > now()
	`, token).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidSession
	}
	return email, err
}

// ValidateEmployeeSession returns the employee's LDAP username for a
// valid, unexpired itp_employee_session token.
func ValidateEmployeeSession(ctx context.Context, db *pgxpool.Pool, token string) (username string, err error) {
	if token == "" {
		return "", ErrInvalidSession
	}
	err = db.QueryRow(ctx, `
		SELECT username FROM employee_sessions WHERE token = $1 AND expires_at > now()
	`, token).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidSession
	}
	return username, err
}
