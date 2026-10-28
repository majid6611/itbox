package api

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
)

const (
	maxMessageChars = 4000

	// A person sending more than rateLimitMax messages in rateLimitWindow
	// is a stuck key or a script, not a conversation.
	rateLimitMax    = 20
	rateLimitWindow = 10 * time.Second
)

func checkLength(content string) error {
	if utf8.RuneCountInString(content) > maxMessageChars {
		return huma.Error400BadRequest("message is too long — max 4000 characters")
	}
	return nil
}

// rateLimiter is a per-user sliding window held in memory, which is right
// for a single chat process (same reasoning as the hub's presence state).
type rateLimiter struct {
	mu   sync.Mutex
	sent map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{sent: make(map[string][]time.Time)}
}

func (r *rateLimiter) allow(username string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	recent := r.sent[username][:0]
	for _, t := range r.sent[username] {
		if now.Sub(t) < rateLimitWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= rateLimitMax {
		r.sent[username] = recent
		return false
	}
	r.sent[username] = append(recent, now)
	return true
}

func (s *Server) checkRate(username string) error {
	if !s.limiter.allow(username, time.Now()) {
		return huma.Error429TooManyRequests("you're sending too fast — wait a few seconds")
	}
	return nil
}
