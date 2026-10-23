package api

import (
	"context"
	"strings"
	"time"

	"it-platform/backend/internal/netbird"
	"it-platform/backend/internal/syslog"
)

// startVpnWatch checks the enrolled-device list once a minute and writes the
// system log when a device joins or disappears. NetBird doesn't tell us
// itself, and removals in our own panel are in the audit log but a device
// joining (a key being used) is not an API call we see.
func (s *Server) startVpnWatch(ctx context.Context) {
	if s.Modules == nil {
		return // tests build a Server without modules
	}
	go func() {
		var known map[string]netbird.Peer // nil until the first successful look
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			known = s.vpnWatchOnce(ctx, known)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (s *Server) vpnWatchOnce(ctx context.Context, known map[string]netbird.Peer) map[string]netbird.Peer {
	nb, ok, err := s.netbirdClient(ctx)
	if err != nil || !ok {
		return known
	}
	peers, err := nb.ListPeers(ctx)
	if err != nil {
		return known
	}
	now := make(map[string]netbird.Peer, len(peers))
	for _, p := range peers {
		now[p.ID] = p
	}
	if known != nil { // the first look only records what's already there
		for id, p := range now {
			if _, seen := known[id]; !seen {
				syslog.Info("vpn-netbird", "Device joined the VPN: %s (%s)", p.Name, deviceOwner(p))
			}
		}
		for id, p := range known {
			if _, still := now[id]; !still {
				syslog.Warn("vpn-netbird", "Device removed from the VPN: %s (%s)", p.Name, deviceOwner(p))
			}
		}
	}
	return now
}

// deviceOwner is the username from a device's "user-<name>" group, or
// "unknown user" for devices enrolled before those groups existed.
func deviceOwner(p netbird.Peer) string {
	for _, g := range p.Groups {
		if name, ok := strings.CutPrefix(g.Name, "user-"); ok {
			return name
		}
	}
	return "unknown user"
}
