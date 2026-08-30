// Package ratelimit provides a tiny in-memory, per-key sliding-window
// limiter — just enough to stop unlimited brute-force attempts against a
// login endpoint. Not meant for anything beyond that: single-instance,
// in-memory (a restart clears it), keyed by client IP.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

// New returns a limiter allowing at most max failed attempts per key
// within window.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{attempts: make(map[string][]time.Time), max: max, window: window}
}

// Allowed reports whether key is currently under its failure limit. Call
// this before attempting the real check (password compare, LDAP bind,
// ...) so a blocked caller can't even trigger that work.
func (l *Limiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.prune(key)
	l.attempts[key] = kept
	return len(kept) < l.max
}

// RecordFailure counts one failed attempt against key.
func (l *Limiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.prune(key), time.Now())
}

// Reset clears key's failure count — call on a successful login so a
// legitimate user who mistyped their password a few times isn't left
// half-throttled.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// prune must be called with l.mu held.
func (l *Limiter) prune(key string) []time.Time {
	cutoff := time.Now().Add(-l.window)
	times := l.attempts[key]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}
