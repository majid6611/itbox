package api

import (
	"strings"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	r := newRateLimiter()
	now := time.Now()
	for i := 0; i < rateLimitMax; i++ {
		if !r.allow("a", now) {
			t.Fatalf("send %d should be allowed", i)
		}
	}
	if r.allow("a", now) {
		t.Fatal("send over the limit should be refused")
	}
	if !r.allow("b", now) {
		t.Fatal("another user is unaffected")
	}
	if !r.allow("a", now.Add(rateLimitWindow+time.Second)) {
		t.Fatal("window should have slid past the old sends")
	}
}

func TestCheckLength(t *testing.T) {
	if checkLength(strings.Repeat("é", maxMessageChars)) != nil {
		t.Fatal("exactly the max (counted in characters, not bytes) is fine")
	}
	if checkLength(strings.Repeat("a", maxMessageChars+1)) == nil {
		t.Fatal("over the max is refused")
	}
}

func TestTypingThrottle(t *testing.T) {
	s := &Server{}
	now := time.Now()
	if !s.typingAllowed("a", now) {
		t.Fatal("first notice goes through")
	}
	if s.typingAllowed("a", now.Add(typingMinGap/2)) {
		t.Fatal("a second one too soon is dropped")
	}
	if !s.typingAllowed("a", now.Add(typingMinGap)) {
		t.Fatal("allowed again after the gap")
	}
	if !s.typingAllowed("b", now) {
		t.Fatal("another user is unaffected")
	}
}
