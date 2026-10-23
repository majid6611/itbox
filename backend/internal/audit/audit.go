// Package audit is the platform's record of who did what: an append-only
// table where every row holds the hash of the row before it, so an edited
// or deleted row is detectable (see Verify).
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Retention is how long entries are kept; the database itself refuses to
// delete anything younger (see migration 0017).
const Retention = 365 * 24 * time.Hour

type Entry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Actor     string    `json:"actor"`
	ActorKind string    `json:"actor_kind"` // admin | employee | "" (not signed in)
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IP        string    `json:"ip"`
	Status    int       `json:"status"`
	Detail    string    `json:"detail"`
}

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// chainLock serialises writers so every row sees the true previous hash.
const chainLock = 7_300_017

// hashEntry covers every field that matters. Times are fixed to UTC
// microseconds because that is all Postgres keeps.
func hashEntry(prev string, e Entry) string {
	h := sha256.New()
	fmt.Fprint(h, strings.Join([]string{
		prev,
		e.At.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		e.Actor, e.ActorKind, e.Action, e.Target, e.IP,
		fmt.Sprint(e.Status), e.Detail,
	}, "\x1f"))
	return hex.EncodeToString(h.Sum(nil))
}

// Record appends one entry. A failure is logged, never returned: an action
// that already happened must not be turned into an error by its own
// bookkeeping. Safe to call on a nil *Service (tests).
func (s *Service) Record(e Entry) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.record(ctx, e); err != nil {
		log.Printf("audit log: couldn't record %s by %q: %v", e.Action, e.Actor, err)
	}
}

func (s *Service) record(ctx context.Context, e Entry) error {
	e.At = time.Now().UTC().Truncate(time.Microsecond)
	e.Detail = clip(e.Detail, 500)
	e.Target = clip(e.Target, 300)
	e.Actor = clip(e.Actor, 200)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, chainLock); err != nil {
		return err
	}
	var prev string
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&prev)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (at, actor, actor_kind, action, target, ip, status, detail, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.At, e.Actor, e.ActorKind, e.Action, e.Target, e.IP, e.Status, e.Detail, prev, hashEntry(prev, e))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type Filter struct {
	Actor  string
	Action string
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
	if f.Actor != "" {
		add("actor ILIKE '%%' || $%d || '%%'", f.Actor)
	}
	if f.Action != "" {
		add("action ILIKE '%%' || $%d || '%%'", f.Action)
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

// List returns one page of entries, newest first, and how many match.
func (s *Service) List(ctx context.Context, f Filter) ([]Entry, int, error) {
	where, args := f.where()
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit, f.Offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT id, at, actor, actor_kind, action, target, ip, status, detail FROM audit_log%s
		ORDER BY id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.ActorKind, &e.Action, &e.Target, &e.IP, &e.Status, &e.Detail); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Each streams every matching entry, oldest first (for the CSV export).
func (s *Service) Each(ctx context.Context, f Filter, fn func(Entry) error) error {
	where, args := f.where()
	rows, err := s.db.Query(ctx, `
		SELECT id, at, actor, actor_kind, action, target, ip, status, detail FROM audit_log`+where+` ORDER BY id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.ActorKind, &e.Action, &e.Target, &e.IP, &e.Status, &e.Detail); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

type VerifyResult struct {
	OK      bool  `json:"ok"`
	Checked int   `json:"checked"`
	BrokeAt int64 `json:"broke_at,omitempty"` // id of the first entry that doesn't fit
	Missing bool  `json:"missing,omitempty"`  // an entry before BrokeAt was removed
}

// Verify walks the whole chain. The oldest remaining entry is the anchor
// (older ones may have aged out), so its own link isn't checked backwards.
func (s *Service) Verify(ctx context.Context) (VerifyResult, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, at, actor, actor_kind, action, target, ip, status, detail, prev_hash, hash
		FROM audit_log ORDER BY id`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()
	res := VerifyResult{OK: true}
	var lastHash string
	for rows.Next() {
		var e Entry
		var prev, hash string
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.ActorKind, &e.Action, &e.Target, &e.IP, &e.Status, &e.Detail, &prev, &hash); err != nil {
			return res, err
		}
		if res.Checked > 0 && prev != lastHash {
			res.OK, res.BrokeAt, res.Missing = false, e.ID, true
			return res, nil
		}
		if hashEntry(prev, e) != hash {
			res.OK, res.BrokeAt = false, e.ID
			return res, nil
		}
		lastHash = hash
		res.Checked++
	}
	return res, rows.Err()
}

// Prune removes entries past retention. The database guard allows nothing
// younger, so a bug here can't shorten the log.
func (s *Service) Prune(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM audit_log WHERE at < $1`, time.Now().Add(-Retention))
	return err
}

// StartPruning runs Prune once a day.
func (s *Service) StartPruning(ctx context.Context) {
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			if err := s.Prune(ctx); err != nil {
				log.Printf("audit log: prune: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
