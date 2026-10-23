package api

import (
	"context"
	"log"
	"net/netip"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type GetOfficeNetworkInput struct {
	SessionToken string `cookie:"itp_session"`
}

type OfficeNetworkOutput struct {
	Body struct {
		// Available is false until the VPN module is installed and
		// bootstrapped — nothing to route through before that.
		Available bool `json:"available"`
		Enabled   bool `json:"enabled"`
		// Network is the saved office LAN (CIDR), "" when off.
		Network string `json:"network"`
		// Ports is what people on the VPN may use on that network:
		// "" for everything, else a list like "tcp/445,tcp/3389,icmp".
		Ports string `json:"ports"`
		// Detected is the LAN this server itself is plugged into — the
		// suggested value when turning the setting on. "" if it couldn't
		// be worked out.
		Detected string `json:"detected"`
	}
}

type SaveOfficeNetworkInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Enabled bool   `json:"enabled"`
		Network string `json:"network"`
		Ports   string `json:"ports"`
	}
}

type SaveOfficeNetworkOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// edgeNetwork is the platform's own Docker network (see docker-compose.yaml)
// — routing it as "the office" would shadow the private-modules route.
var edgeNetwork = netip.MustParsePrefix("10.201.28.0/24")

// detectOfficeNetwork reads the host's routing table and returns the
// directly-connected subnet of the interface its default route uses —
// i.e. the LAN the server is plugged into.
func (s *Server) detectOfficeNetwork(ctx context.Context) string {
	out, err := s.Docker.HostRoutes(ctx)
	if err != nil {
		log.Printf("detect office network: %v", err)
		return ""
	}
	return parseOfficeNetwork(out)
}

func parseOfficeNetwork(routes string) string {
	var defaultDev string
	lines := strings.Split(routes, "\n")
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 5 && f[0] == "default" {
			for i := 0; i+1 < len(f); i++ {
				if f[i] == "dev" {
					defaultDev = f[i+1]
				}
			}
			break
		}
	}
	if defaultDev == "" {
		return ""
	}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 3 || !strings.Contains(f[0], "/") || !strings.Contains(l, "dev "+defaultDev+" ") {
			continue
		}
		p, err := netip.ParsePrefix(f[0])
		if err == nil && p.Addr().Is4() && p.Addr().IsPrivate() {
			return p.Masked().String()
		}
	}
	return ""
}

// validOfficeNetwork accepts a private IPv4 subnet between /8 and /30 —
// anything public would mean "route part of the internet through the
// office," never what an admin means here.
func validOfficeNetwork(s string) (netip.Prefix, bool) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() < 8 || p.Bits() > 30 {
		return netip.Prefix{}, false
	}
	p = p.Masked()
	if p.Overlaps(edgeNetwork) {
		return netip.Prefix{}, false
	}
	return p, true
}

// SyncGatewayRoutesOnStart runs syncGatewayRoutes once at backend start,
// retrying while the VPN module's own containers finish coming up — see
// syncGatewayRoutes for why this needs to happen on every start.
func (s *Server) SyncGatewayRoutesOnStart() {
	ctx := context.Background()
	for attempt := 0; attempt < 20; attempt++ {
		err := s.syncGatewayRoutes(ctx)
		if err == nil {
			return
		}
		if attempt == 19 {
			log.Printf("gateway routes: giving up at startup: %v", err)
			return
		}
		time.Sleep(15 * time.Second)
	}
}

func registerOfficeNetwork(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-vpn-office-network",
		Method:      "GET",
		Path:        "/api/vpn/office-network",
		Summary:     "Whether VPN users can reach the office LAN, and which subnet",
	}, func(ctx context.Context, in *GetOfficeNetworkInput) (*OfficeNetworkOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &OfficeNetworkOutput{}
		_, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		out.Body.Available = available
		if err := s.DB.QueryRow(ctx, `SELECT vpn_office_network, vpn_office_ports FROM platform_settings WHERE id = true`).Scan(&out.Body.Network, &out.Body.Ports); err != nil {
			return nil, internalError("load office network", err)
		}
		out.Body.Enabled = out.Body.Network != ""
		out.Body.Detected = s.detectOfficeNetwork(ctx)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-vpn-office-network",
		Method:      "PUT",
		Path:        "/api/vpn/office-network",
		Summary:     "Turn VPN access to the office LAN on or off",
	}, func(ctx context.Context, in *SaveOfficeNetworkInput) (*SaveOfficeNetworkOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		network, ports := "", ""
		if in.Body.Enabled {
			if _, err := parseOfficePorts(in.Body.Ports); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			ports = strings.TrimSpace(in.Body.Ports)
			p, ok := validOfficeNetwork(in.Body.Network)
			if !ok {
				return nil, huma.Error400BadRequest("enter a private office network like 192.168.1.0/24")
			}
			network = p.String()
		}
		if _, err := s.DB.Exec(ctx, `UPDATE platform_settings SET vpn_office_network = $1, vpn_office_ports = $2 WHERE id = true`, network, ports); err != nil {
			return nil, internalError("save office network", err)
		}
		if err := s.syncGatewayRoutes(ctx); err != nil {
			return nil, internalError("update vpn routes", err)
		}
		out := &SaveOfficeNetworkOutput{}
		out.Body.Success = true
		return out, nil
	})
}
