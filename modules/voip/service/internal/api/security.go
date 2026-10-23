package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"it-platform/voip/internal/ami"
	"it-platform/voip/internal/dbx"
)

// failureNoteEvery is how often one address may appear in the system log. A
// scanner on the internet can fail thousands of times an hour; the log gets
// one line per address per interval, carrying the running count.
const failureNoteEvery = 10 * time.Minute

type failureTracker struct {
	mu     sync.Mutex
	seen   map[string]*failures
	recent map[string]time.Time // source address and port -> when last counted
}

type failures struct {
	count  int
	logged time.Time
}

// reasons for the Asterisk security events that mean "somebody was turned
// away". Everything else (a successful sign-in, the challenge every phone
// gets first) is not a failure.
var failureReasons = map[string]string{
	"InvalidAccountID":        "no such extension",
	"InvalidPassword":         "wrong password",
	"ChallengeResponseFailed": "wrong password",
	"FailedACL":               "not on the office network or the VPN",
	"AuthMethodNotAllowed":    "sign-in method not allowed",
	"UnexpectedAddress":       "unexpected address",
}

// note records one security event and returns the system-log line to write,
// or "" when it's not a failure or this address was logged recently.
func (t *failureTracker) note(ev ami.Message, now time.Time) string {
	reason, ok := failureReasons[ev["Event"]]
	if !ok {
		return ""
	}
	addr := remoteIP(ev["RemoteAddress"])
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = map[string]*failures{}
		t.recent = map[string]time.Time{}
	}
	// Asterisk reports one failed attempt as two or three events, all from
	// the same source port; count the attempt once.
	for k, at := range t.recent {
		if now.Sub(at) > 10*time.Second {
			delete(t.recent, k)
		}
	}
	if _, dup := t.recent[ev["RemoteAddress"]]; dup {
		return ""
	}
	t.recent[ev["RemoteAddress"]] = now
	for k, f := range t.seen { // forget addresses quiet for an hour
		if now.Sub(f.logged) > time.Hour {
			delete(t.seen, k)
		}
	}
	f := t.seen[addr]
	if f == nil {
		f = &failures{}
		t.seen[addr] = f
	}
	f.count++
	if f.count > 1 && now.Sub(f.logged) < failureNoteEvery {
		return ""
	}
	f.logged = now
	who := "extension " + ev["AccountID"]
	if ev["AccountID"] == "" {
		who = "unknown extension"
	}
	return fmt.Sprintf("Phone sign-in failed from %s (%s: %s). %d failed so far from this address.", addr, who, reason, f.count)
}

// remoteIP pulls the address out of Asterisk's "IPV4/UDP/203.0.113.9/5060".
func remoteIP(remote string) string {
	parts := strings.Split(remote, "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return remote
}

// WatchSecurity writes failed phone sign-ins to the platform's system log so
// the admin can see someone guessing at the phone system. Asterisk reports
// them as security events over the same manager connection used elsewhere.
func (s *Server) WatchSecurity(ctx context.Context) {
	var t failureTracker
	s.AMI.Listen(ctx, func(ev ami.Message) {
		if line := t.note(ev, time.Now()); line != "" {
			dbx.SystemLog("warn", "%s", line)
		}
	})
}
