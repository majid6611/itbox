package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/netbird"
)

// bootstrapNetbird runs once, right after vpn-netbird's containers start.
// It's the one-time setup NetBird's API requires that config.yaml can't
// do: create the first account (to get a management token) and register
// our Dex sidecar as the login connector. Polls because the container can
// take a couple of minutes on first boot (it downloads a GeoIP database
// before its HTTP server starts listening).
func (s *Server) bootstrapNetbird(ctx context.Context) {
	deadline := time.Now().Add(5 * time.Minute)
	var status struct{ Config map[string]string }
	for time.Now().Before(deadline) {
		st, ok, err := s.Modules.GetInstalled(ctx, "vpn-netbird")
		if err == nil && ok && st.Status == "running" {
			status.Config = st.Config
			break
		}
		time.Sleep(5 * time.Second)
	}
	if status.Config == nil {
		log.Printf("netbird bootstrap: module never reached running, giving up")
		return
	}

	addr := s.Modules.ServiceAddr("vpn-netbird", "netbird-server", 80)
	nb := netbird.NewClient("http://" + addr)

	var pat string
	for time.Now().Before(deadline) {
		p, err := nb.Setup(ctx, status.Config["owner_email"], status.Config["owner_password"], "Admin")
		if err == nil {
			pat = p
			break
		}
		time.Sleep(5 * time.Second)
	}
	if pat == "" {
		log.Printf("netbird bootstrap: setup never succeeded, giving up")
		return
	}
	nb.SetToken(pat)

	dexIssuer := "http://" + s.Modules.Hostname("vpn-netbird", "dex") + ":8000"
	if err := nb.RegisterIdentityProvider(ctx, "Company LDAP", dexIssuer, "netbird", status.Config["dex_client_secret"]); err != nil {
		log.Printf("netbird bootstrap: register identity provider: %v", err)
		// Still persist the token below — the admin can be told to retry
		// registration, but losing the token entirely means starting over.
	}

	_, err := s.DB.Exec(ctx,
		`UPDATE installed_modules SET config = config || jsonb_build_object('management_pat', $2::text) WHERE module_id = $1`,
		"vpn-netbird", pat)
	if err != nil {
		log.Printf("netbird bootstrap: persist management token: %v", err)
		return
	}
	log.Printf("netbird bootstrap: complete")
}

// netbirdClient returns a ready-to-use API client if vpn-netbird is
// installed, running, and has finished its one-time bootstrap (management
// token present) — available=false otherwise, e.g. "still bootstrapping"
// right after install.
func (s *Server) netbirdClient(ctx context.Context) (client *netbird.Client, available bool, err error) {
	status, ok, err := s.Modules.GetInstalled(ctx, "vpn-netbird")
	if err != nil {
		return nil, false, err
	}
	if !ok || status.Status != "running" {
		return nil, false, nil
	}
	pat := status.Config["management_pat"]
	if pat == "" {
		return nil, false, nil
	}
	addr := s.Modules.ServiceAddr("vpn-netbird", "netbird-server", 80)
	client = netbird.NewClient("http://" + addr)
	client.SetToken(pat)
	return client, true, nil
}

type ListVpnUsersInput struct {
	SessionToken string `cookie:"itp_session"`
}

type VpnUserOut struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	HasAccess bool   `json:"has_access"`
}

type ListVpnUsersOutput struct {
	Body struct {
		// Available is false until both the Identity module and the VPN
		// module are installed, running, and (for VPN) finished bootstrapping.
		Available bool `json:"available"`
		// DomainConfigured is false while the platform domain is still the
		// "localhost" default (see Settings) — a setup file built with
		// that would only ever work on the admin's own machine, so
		// enable/download are refused until a real domain is set.
		DomainConfigured bool         `json:"domain_configured"`
		Users            []VpnUserOut `json:"users"`
	}
}

// domainConfigured is false while the platform is still on the
// "localhost" default — see BaseDomain's doc comment on why that can
// never work for anyone but the admin's own machine.
func (s *Server) domainConfigured() bool {
	return s.Modules.BaseDomain() != "localhost"
}

type VpnUsernameInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
}

type EnableVpnOutput struct {
	Body struct {
		Success  bool   `json:"success"`
		SetupKey string `json:"setup_key"`
	}
}

func registerVpn(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-vpn-users",
		Method:      "GET",
		Path:        "/api/vpn/users",
		Summary:     "List company users with their VPN enrollment status",
	}, func(ctx context.Context, in *ListVpnUsersInput) (*ListVpnUsersOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &ListVpnUsersOutput{}

		dirClient, dirAvailable, err := s.directoryClient(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check identity module", err)
		}
		_, nbAvailable, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check vpn module", err)
		}
		out.Body.Available = dirAvailable && nbAvailable
		out.Body.DomainConfigured = s.domainConfigured()
		if !out.Body.Available {
			return out, nil
		}

		users, err := dirClient.ListUsers()
		if err != nil {
			return nil, huma.Error500InternalServerError("list users", err)
		}
		rows, err := s.DB.Query(ctx, `SELECT username FROM vpn_access`)
		if err != nil {
			return nil, huma.Error500InternalServerError("list vpn access", err)
		}
		defer rows.Close()
		hasAccess := make(map[string]bool)
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				return nil, huma.Error500InternalServerError("scan vpn access", err)
			}
			hasAccess[u] = true
		}

		for _, u := range users {
			out.Body.Users = append(out.Body.Users, VpnUserOut{Username: u.Username, Name: u.Name, HasAccess: hasAccess[u.Username]})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "enable-vpn-user",
		Method:      "POST",
		Path:        "/api/vpn/users/{username}/enable",
		Summary:     "Give a company user VPN access (issues an enrollment setup key)",
	}, func(ctx context.Context, in *VpnUsernameInput) (*EnableVpnOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if !s.domainConfigured() {
			return nil, huma.Error400BadRequest("set a real domain name in Settings first — a setup file built with \"localhost\" would only ever work on this computer")
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first (or it's still finishing setup — try again in a minute)")
		}
		key, err := nb.CreateSetupKey(ctx, in.Username)
		if err != nil {
			return nil, huma.Error500InternalServerError("create setup key", err)
		}
		_, err = s.DB.Exec(ctx, `
			INSERT INTO vpn_access (username, setup_key_id, setup_key)
			VALUES ($1, $2, $3)
			ON CONFLICT (username) DO UPDATE SET setup_key_id = $2, setup_key = $3, created_at = now()
		`, in.Username, key.ID, key.Key)
		if err != nil {
			return nil, huma.Error500InternalServerError("record vpn access", err)
		}
		out := &EnableVpnOutput{}
		out.Body.Success = true
		out.Body.SetupKey = key.Key
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disable-vpn-user",
		Method:      "POST",
		Path:        "/api/vpn/users/{username}/disable",
		Summary:     "Revoke a company user's VPN enrollment key",
	}, func(ctx context.Context, in *VpnUsernameInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first")
		}
		var keyID string
		err = s.DB.QueryRow(ctx, `SELECT setup_key_id FROM vpn_access WHERE username = $1`, in.Username).Scan(&keyID)
		if err == nil {
			if err := nb.DeleteSetupKey(ctx, keyID); err != nil {
				log.Printf("delete setup key for %s: %v", in.Username, err)
			}
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM vpn_access WHERE username = $1`, in.Username); err != nil {
			return nil, huma.Error500InternalServerError("remove vpn access record", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "download-vpn-setup",
		Method:      "GET",
		Path:        "/api/vpn/users/{username}/download",
		Summary:     "Download setup instructions with the user's VPN enrollment key",
	}, func(ctx context.Context, in *VpnUsernameInput) (*huma.StreamResponse, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if !s.domainConfigured() {
			return nil, huma.Error400BadRequest("set a real domain name in Settings first — a setup file built with \"localhost\" would only ever work on this computer")
		}
		var setupKey string
		err := s.DB.QueryRow(ctx, `SELECT setup_key FROM vpn_access WHERE username = $1`, in.Username).Scan(&setupKey)
		if err != nil {
			return nil, huma.Error404NotFound("no VPN access for this user yet")
		}
		// Port 8090, not the shared edge port (8000) — netbird-server's
		// client protocol is native gRPC and gets its own direct host port
		// instead of going through nginx. See proxy/nginx.go.
		managementURL := "http://" + s.Modules.Hostname("vpn-netbird", "") + ":8090"
		text := fmt.Sprintf(`Company VPN setup for %s
==============================================

1. Install the NetBird app:
   https://netbird.io/download

2. Open the app and choose "Use your own server" / advanced setup.
   Server (Management URL): %s

3. When asked for a setup key, paste this:
   %s

That's it — no username or password needed, just this one key.
If it stops working, ask your admin for a new one.
`, in.Username, managementURL, setupKey)

		return &huma.StreamResponse{
			Body: func(ctx huma.Context) {
				ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
				ctx.SetHeader("Content-Disposition", fmt.Sprintf(`attachment; filename="vpn-setup-%s.txt"`, in.Username))
				ctx.BodyWriter().Write([]byte(text))
			},
		}, nil
	})
}
