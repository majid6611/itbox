package api

import (
	"context"
	"log"
	"strings"
	"time"
)

// bootstrapInternalGateway enrolls the platform's own internal-gateway
// container as a NetBird peer, then tells NetBird "this peer can reach
// 10.201.28.2" (nginx's fixed edge-network address) — the one mechanism
// behind every private module: a VPN-connected device gets routed to that
// address only because this peer said so, not because it's on the public
// internet. Runs every time vpn-netbird finishes installing (chained
// after bootstrapNetbird, same as that function's own trigger) because a
// fresh NetBird account has no peers or routes of its own — see
// bootstrapNetbird's doc comment for why that happens on every install,
// not just the first.
func (s *Server) bootstrapInternalGateway(ctx context.Context) {
	deadline := time.Now().Add(5 * time.Minute)

	var pat string
	for time.Now().Before(deadline) {
		status, ok, err := s.Modules.GetInstalled(ctx, "vpn-netbird")
		if err == nil && ok && status.Status == "running" && status.Config["management_pat"] != "" {
			pat = status.Config["management_pat"]
			break
		}
		time.Sleep(5 * time.Second)
	}
	if pat == "" {
		log.Printf("internal gateway bootstrap: vpn-netbird never finished bootstrapping, giving up")
		return
	}

	client, available, err := s.netbirdClient(ctx)
	if err != nil || !available {
		log.Printf("internal gateway bootstrap: netbird client unavailable: %v", err)
		return
	}

	key, err := client.CreateSetupKey(ctx, "internal-gateway")
	if err != nil {
		log.Printf("internal gateway bootstrap: create setup key: %v", err)
		return
	}

	mgmtURL := "http://" + s.Modules.Hostname("vpn-netbird", "") + ":8090"

	// Best-effort: only matters if this is a re-bootstrap after
	// vpn-netbird was reinstalled and the gateway was still holding a
	// connection to the now-dead old account.
	_, _ = s.Docker.Exec(ctx, s.GatewayContainerName, "netbird", "down")

	for attempt := 0; attempt < 10; attempt++ {
		_, err = s.Docker.Exec(ctx, s.GatewayContainerName, "netbird", "up", "--management-url", mgmtURL, "--setup-key", key.Key)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Printf("internal gateway bootstrap: netbird up: %v", err)
		return
	}

	peers, err := client.ListPeers(ctx)
	if err != nil {
		log.Printf("internal gateway bootstrap: list peers: %v", err)
		return
	}
	var peerID string
	for _, p := range peers {
		if p.Hostname == s.GatewayContainerName {
			peerID = p.ID
			break
		}
	}
	if peerID == "" {
		log.Printf("internal gateway bootstrap: gateway peer not found among %d peers (looked for hostname %q)", len(peers), s.GatewayContainerName)
		return
	}

	groups, err := client.ListGroups(ctx)
	if err != nil {
		log.Printf("internal gateway bootstrap: list groups: %v", err)
		return
	}
	var allGroupID string
	for _, g := range groups {
		if strings.EqualFold(g.Name, "All") {
			allGroupID = g.ID
			break
		}
	}
	if allGroupID == "" {
		log.Printf("internal gateway bootstrap: no \"All\" group found")
		return
	}

	if err := client.CreateRoute(ctx, internalServicesCIDR, peerID, allGroupID); err != nil {
		log.Printf("internal gateway bootstrap: create route: %v", err)
		return
	}

	log.Printf("internal gateway bootstrap: complete")
}

// internalServicesCIDR is nginx's fixed edge-network address (see
// docker-compose.yaml), advertised as a /32 route — just that one host,
// not a wider subnet, since it's the single front door every private
// module's traffic passes through regardless of which port it's on.
const internalServicesCIDR = "10.201.28.2/32"
