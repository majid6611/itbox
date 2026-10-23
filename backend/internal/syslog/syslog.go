// Package syslog is the platform's system log: a table of what the platform
// and its modules did, for an admin looking into a problem. Modules that
// share the platform's database write to the same table (see README).
package syslog

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Retention is how long entries are kept.
const Retention = 90 * 24 * time.Hour

type Event struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Source  string    `json:"source"`
	Message string    `json:"message"`
}

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// Default is the log the Info/Warn/Error helpers write to; main sets it.
// A package-level default keeps the call sites (deep inside module
// lifecycle code, error helpers, ...) to one line each. Nil is fine: the
// helpers then only write to the process log.
var Default *Service

func Info(source, format string, args ...any)  { Default.write("info", source, format, args) }
func Warn(source, format string, args ...any)  { Default.write("warn", source, format, args) }
func Error(source, format string, args ...any) { Default.write("error", source, format, args) }

// write never fails the caller: a problem recording a diagnostic must not
// become a second problem.
func (s *Service) write(level, source, format string, args []any) {
	msg := clip(fmt.Sprintf(format, args...), 2000)
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.Exec(ctx, `INSERT INTO system_log (level, source, message) VALUES ($1, $2, $3)`, level, source, msg); err != nil {
		log.Printf("system log: couldn't record %q: %v", msg, err)
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type Filter struct {
	Level  string // "" = all; "warn" = warn and error; "error" = errors only
	Source string
	Search string
	From   time.Time
	To     time.Time
	Limit  int
	Offset int
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	switch f.Level {
	case "warn":
		conds = append(conds, "level IN ('warn','error')")
	case "error":
		conds = append(conds, "level = 'error'")
	}
	if f.Source != "" {
		add("source = $%d", f.Source)
	}
	if f.Search != "" {
		add("message ILIKE '%%' || $%d || '%%'", f.Search)
	}
	if !f.From.IsZero() {
		add("at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("at < $%d", f.To)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *Service) List(ctx context.Context, f Filter) ([]Event, int, error) {
	where, args := f.where()
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM system_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit, f.Offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT id, at, level, source, message FROM system_log%s
		ORDER BY id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.Level, &e.Source, &e.Message); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Each streams every matching entry, oldest first (for the CSV export).
func (s *Service) Each(ctx context.Context, f Filter, fn func(Event) error) error {
	where, args := f.where()
	rows, err := s.db.Query(ctx, `SELECT id, at, level, source, message FROM system_log`+where+` ORDER BY id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.Level, &e.Source, &e.Message); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Sources lists who has written to the log, for the page's filter.
func (s *Service) Sources(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT source FROM system_log ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Service) Prune(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM system_log WHERE at < $1`, time.Now().Add(-Retention))
	return err
}

// StartPruning runs Prune once a day.
func (s *Service) StartPruning(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			if err := s.Prune(ctx); err != nil {
				log.Printf("system log: prune: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
