package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"it-platform/backend/internal/netbird"
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

	approvedID, err := s.ensureAccessPolicy(ctx, client)
	if err != nil {
		log.Printf("internal gateway bootstrap: access policy: %v", err)
		return
	}
	// The gateway is ours: it joins the approved group straight away.
	key, err := client.CreateReusableSetupKey(ctx, "internal-gateway", []string{approvedID})
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

	// The gateway peer can take a moment to show up after `netbird up`.
	for attempt := 0; attempt < 10; attempt++ {
		if err = s.syncGatewayRoutes(ctx); err == nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		log.Printf("internal gateway bootstrap: %v", err)
		return
	}

	log.Printf("internal gateway bootstrap: complete")
}

// Route names (NetBird network_id) for the two things the gateway can
// advertise — syncGatewayRoutes owns every route with one of these names.
const (
	internalServicesRouteID = "internal-services"
	officeNetworkRouteID    = "office-network"
)

// syncGatewayRoutes makes NetBird's routes through the internal gateway
// match what they should be right now: always nginx's address (private
// modules), plus the office LAN if an admin turned that on. Converges
// rather than just creating, so it's safe to run any time — at install,
// at every backend start, and on each office-network settings save. The
// at-start run is what repairs a route left pointing at an old address
// (e.g. after the edge network's subnet changed), which a create-once
// bootstrap never revisits.
func (s *Server) syncGatewayRoutes(ctx context.Context) error {
	client, available, err := s.netbirdClient(ctx)
	if err != nil {
		return fmt.Errorf("netbird client: %w", err)
	}
	if !available {
		return nil // VPN not installed/bootstrapped — nothing to route
	}

	var officeNetwork string
	if err := s.DB.QueryRow(ctx, `SELECT vpn_office_network FROM platform_settings WHERE id = true`).Scan(&officeNetwork); err != nil {
		return fmt.Errorf("load office network setting: %w", err)
	}
	want := map[string]string{internalServicesRouteID: internalServicesCIDR}
	if officeNetwork != "" {
		want[officeNetworkRouteID] = officeNetwork
	}

	groupID, err := s.ensureAccessPolicy(ctx, client)
	if err != nil {
		return err
	}
	// A reinstalled VPN module starts with a fresh account, so the 2FA
	// setting is re-applied here as well. Not worth failing the route
	// repair over.
	if err := client.SetLocalMFA(ctx, s.vpnMFAWanted(ctx)); err != nil {
		log.Printf("gateway routes: apply 2fa setting: %v", err)
	}
	peerID, err := s.gatewayPeer(ctx, client)
	if err != nil {
		return err
	}

	routes, err := client.ListRoutes(ctx)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range routes {
		if r.NetworkID != internalServicesRouteID && r.NetworkID != officeNetworkRouteID {
			continue // not ours
		}
		if want[r.NetworkID] == r.Network && r.Peer == peerID && len(r.Groups) == 1 && r.Groups[0] == groupID && !have[r.NetworkID] {
			have[r.NetworkID] = true
			continue
		}
		if err := client.DeleteRoute(ctx, r.ID); err != nil {
			return err
		}
		log.Printf("gateway routes: removed %s (%s)", r.NetworkID, r.Network)
	}
	for id, cidr := range want {
		if have[id] {
			continue
		}
		desc := "internal services gateway"
		if id == officeNetworkRouteID {
			desc = "office network"
		}
		if err := client.CreateRoute(ctx, id, cidr, desc, peerID, groupID); err != nil {
			return fmt.Errorf("create route %s: %w", id, err)
		}
		log.Printf("gateway routes: added %s (%s)", id, cidr)
	}
	return nil
}

// gatewayPeer finds the internal gateway's own NetBird peer.
func (s *Server) gatewayPeer(ctx context.Context, client *netbird.Client) (string, error) {
	peers, err := client.ListPeers(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range peers {
		if p.Hostname == s.GatewayContainerName {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("gateway peer not found among %d peers (looked for hostname %q)", len(peers), s.GatewayContainerName)
}

// internalServicesCIDR is nginx's fixed edge-network address (see
// docker-compose.yaml), advertised as a /32 route — just that one host,
// not a wider subnet, since it's the single front door every private
// module's traffic passes through regardless of which port it's on.
const internalServicesCIDR = "10.201.28.2/32"
