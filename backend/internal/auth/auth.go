package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

const sessionTTL = 7 * 24 * time.Hour

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// EnsureAdmin seeds the first admin user from env-provided credentials if
// no admin users exist yet. Safe to call on every boot.
func (s *Service) EnsureAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO admin_users (email, password_hash) VALUES ($1, $2)`, email, string(hash))
	return err
}

func (s *Service) Login(ctx context.Context, email, password string) (token string, err error) {
	var id, hash string
	err = s.db.QueryRow(ctx, `SELECT id, password_hash FROM admin_users WHERE email = $1`, email).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", ErrInvalidCredentials
	}

	token, err = randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO sessions (token, admin_user_id, expires_at) VALUES ($1, $2, $3)`,
		token, id, time.Now().Add(sessionTTL),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// ValidateSession returns the admin user's email for a valid, unexpired token.
func (s *Service) ValidateSession(ctx context.Context, token string) (email string, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT admin_users.email FROM sessions
		JOIN admin_users ON admin_users.id = sessions.admin_user_id
		WHERE sessions.token = $1 AND sessions.expires_at > now()
	`, token).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("session not found or expired")
	}
	return email, err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
