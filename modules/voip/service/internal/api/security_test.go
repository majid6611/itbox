package api

import (
	"strings"
	"testing"
	"time"

	"it-platform/voip/internal/ami"
)

func TestFailureTrackerOneLinePerAddressPerInterval(t *testing.T) {
	var tr failureTracker
	now := time.Now()
	bad := ami.Message{"Event": "InvalidPassword", "AccountID": "101", "RemoteAddress": "IPV4/UDP/203.0.113.9/5060"}

	line := tr.note(bad, now)
	if !strings.Contains(line, "203.0.113.9") || !strings.Contains(line, "extension 101") || !strings.Contains(line, "wrong password") || !strings.Contains(line, "1 failed") {
		t.Fatalf("first failure should be logged with who and why: %q", line)
	}
	if got := tr.note(bad, now.Add(time.Minute)); got != "" {
		t.Fatalf("second failure inside the interval should stay quiet: %q", got)
	}
	if got := tr.note(ami.Message{"Event": "FailedACL", "RemoteAddress": "IPV4/UDP/198.51.100.4/5060"}, now.Add(time.Minute)); got == "" {
		t.Fatal("a different address is its own entry")
	}
	if got := tr.note(bad, now.Add(11*time.Minute)); !strings.Contains(got, "3 failed") {
		t.Fatalf("after the interval the address is logged again with the running count: %q", got)
	}
}

func TestFailureTrackerCountsAnAttemptOnce(t *testing.T) {
	var tr failureTracker
	now := time.Now()
	// what Asterisk really sends for one wrong password: the same source port twice
	first := ami.Message{"Event": "InvalidAccountID", "AccountID": "999", "RemoteAddress": "IPV4/UDP/203.0.113.9/41579"}
	second := ami.Message{"Event": "ChallengeResponseFailed", "AccountID": "<unknown>", "RemoteAddress": "IPV4/UDP/203.0.113.9/41579"}
	if tr.note(first, now) == "" {
		t.Fatal("first event should be logged")
	}
	tr.note(second, now)
	again := ami.Message{"Event": "InvalidAccountID", "AccountID": "998", "RemoteAddress": "IPV4/UDP/203.0.113.9/50000"}
	if got := tr.note(again, now.Add(11*time.Minute)); !strings.Contains(got, "2 failed") {
		t.Fatalf("two attempts, not four: %q", got)
	}
}

func TestFailureTrackerIgnoresEverythingThatIsNotAFailure(t *testing.T) {
	var tr failureTracker
	for _, ev := range []string{"SuccessfulAuth", "ChallengeSent", "InvalidTransport"} {
		if got := tr.note(ami.Message{"Event": ev, "RemoteAddress": "IPV4/UDP/10.0.0.5/5060"}, time.Now()); got != "" {
			t.Fatalf("%s is not a failed sign-in: %q", ev, got)
		}
	}
}
