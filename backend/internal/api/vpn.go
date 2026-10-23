package api

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/netbird"
	"it-platform/backend/internal/syslog"
)

// bootstrapNetbird runs once, right after vpn-netbird's containers start.
// It's the one-time setup NetBird's API requires that config.yaml can't
// do: create the first account, to get a management token our own panel
// uses for everything else (setup keys, peer list, routes). Polls because
// the container can take a couple of minutes on first boot (it downloads
// a GeoIP database before its HTTP server starts listening).
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
	// KeyExpiresAt is when the current setup key stops working ("" if
	// the user has none or it's an older key with no recorded expiry).
	KeyExpiresAt string `json:"key_expires_at"`
	// Login is true when the person signs in with a password and an
	// authenticator code (2FA) instead of a setup key.
	Login bool `json:"login"`
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
		// Devices is every device NetBird currently knows about. Each
		// carries its "user-<name>" group, which says whose it is.
		Devices []netbird.Peer `json:"devices"`
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

type VpnDeviceInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           string `path:"id"`
}

type EnableVpnOutput struct {
	Body struct {
		Success  bool   `json:"success"`
		SetupKey string `json:"setup_key"`
		// LoginEmail and LoginPassword are set instead of SetupKey when 2FA
		// is on.
		LoginEmail    string `json:"login_email"`
		LoginPassword string `json:"login_password"`
	}
}

// userVpnGroup finds (or, with create, makes) the personal NetBird group
// every device enrolled with a user's setup key joins. NetBird doesn't say
// which key enrolled a peer, so this group is the only record of whose
// device is whose.
func userVpnGroup(ctx context.Context, nb *netbird.Client, username string, create bool) (string, error) {
	name := "user-" + username
	groups, err := nb.ListGroups(ctx)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == name {
			return g.ID, nil
		}
	}
	if !create {
		return "", nil
	}
	g, err := nb.CreateGroup(ctx, name)
	if err != nil {
		return "", err
	}
	return g.ID, nil
}

const (
	approvedGroupName  = "vpn-approved"
	approvedPolicyName = "approved devices"
)

var accessPolicyMu sync.Mutex

// ensureAccessPolicy makes a newly enrolled device useless until an admin
// approves it, and returns the ID of the approved group. A device can reach
// other devices (and the office network, and private modules) only when it
// is in that group; enrolling with a setup key doesn't put it there.
//
// NetBird starts every account with a "Default" policy that lets everything
// talk to everything. The first time this runs against such an account it
// moves every device already enrolled into the approved group, so nobody
// loses access, and then deletes that policy.
func (s *Server) ensureAccessPolicy(ctx context.Context, nb *netbird.Client) (string, error) {
	accessPolicyMu.Lock()
	defer accessPolicyMu.Unlock()

	groups, err := nb.ListGroups(ctx)
	if err != nil {
		return "", err
	}
	var approved *netbird.Group
	for i := range groups {
		if groups[i].Name == approvedGroupName {
			approved = &groups[i]
		}
	}
	if approved == nil {
		if approved, err = nb.CreateGroup(ctx, approvedGroupName); err != nil {
			return "", err
		}
	}

	policies, err := nb.ListPolicies(ctx)
	if err != nil {
		return "", err
	}
	hasApproved, defaultID := false, ""
	for _, p := range policies {
		switch p.Name {
		case approvedPolicyName:
			hasApproved = true
		case "Default":
			defaultID = p.ID
		}
	}
	if !hasApproved {
		if err := nb.CreateAllowPolicy(ctx, approvedPolicyName, approved.ID); err != nil {
			return "", err
		}
	}
	if defaultID != "" {
		if err := nb.AddAllPeersToGroup(ctx, *approved); err != nil {
			return "", err
		}
		if err := nb.DeletePolicy(ctx, defaultID); err != nil {
			return "", err
		}
		syslog.Info("vpn-netbird", "New devices now need admin approval; devices already enrolled were approved")
	}
	return approved.ID, nil
}

// revokeVpnAccess takes a user's VPN away completely: the enrollment key
// (no new devices), then every device already enrolled with it (kicked off
// the network now, not whenever they next reconnect). Devices enrolled
// before groups existed have no group and can only be removed by hand in
// NetBird. Safe to call for a user with no VPN access.
func (s *Server) revokeVpnAccess(ctx context.Context, nb *netbird.Client, username string) error {
	var keyID, userID string
	if err := s.DB.QueryRow(ctx, `SELECT setup_key_id, nb_user_id FROM vpn_access WHERE username = $1`, username).Scan(&keyID, &userID); err == nil {
		if keyID != "" {
			if err := nb.DeleteSetupKey(ctx, keyID); err != nil {
				log.Printf("delete setup key for %s: %v", username, err)
			}
		}
		if userID != "" {
			if err := nb.DeleteUser(ctx, userID); err != nil {
				log.Printf("delete vpn login for %s: %v", username, err)
			}
		}
	}

	groupID, err := userVpnGroup(ctx, nb, username, false)
	if err != nil {
		return err
	}
	if groupID != "" {
		peers, err := nb.ListPeers(ctx)
		if err != nil {
			return err
		}
		for _, p := range peers {
			for _, g := range p.Groups {
				if g.ID == groupID {
					if err := nb.DeletePeer(ctx, p.ID); err != nil {
						return err
					}
					break
				}
			}
		}
		if err := nb.DeleteGroup(ctx, groupID); err != nil {
			log.Printf("delete vpn group for %s: %v", username, err)
		}
	}

	_, err = s.DB.Exec(ctx, `DELETE FROM vpn_access WHERE username = $1`, username)
	return err
}

// vpnTwoFactor reports whether new VPN access is issued as a login with an
// authenticator code (2FA) rather than a setup key. Off by default.
func (s *Server) vpnTwoFactor(ctx context.Context) bool {
	var on bool
	_ = s.DB.QueryRow(ctx, `SELECT vpn_two_factor FROM platform_settings WHERE id = true`).Scan(&on)
	return on
}

// vpnMFAWanted is whether NetBird should ask for the authenticator code:
// when the option is on, and also while anyone still signs in with a login —
// turning the option off only stops issuing new logins, it must not quietly
// drop the code requirement for the people who already have one.
func (s *Server) vpnMFAWanted(ctx context.Context) bool {
	if s.vpnTwoFactor(ctx) {
		return true
	}
	var any bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM vpn_access WHERE nb_user_id <> '')`).Scan(&any)
	return any
}

type VpnTwoFactorInput struct {
	SessionToken string `cookie:"itp_session"`
}

type VpnTwoFactorOutput struct {
	Body struct {
		Enabled bool `json:"enabled"`
	}
}

type SaveVpnTwoFactorInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Enabled bool `json:"enabled"`
	}
}

func registerVpn(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-vpn-two-factor",
		Method:      "GET",
		Path:        "/api/vpn/two-factor",
		Summary:     "Whether VPN access is issued with 2FA (password + authenticator code)",
	}, func(ctx context.Context, in *VpnTwoFactorInput) (*VpnTwoFactorOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &VpnTwoFactorOutput{}
		out.Body.Enabled = s.vpnTwoFactor(ctx)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-vpn-two-factor",
		Method:      "PUT",
		Path:        "/api/vpn/two-factor",
		Summary:     "Turn 2FA for newly issued VPN access on or off",
	}, func(ctx context.Context, in *SaveVpnTwoFactorInput) (*VpnTwoFactorOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first")
		}
		if _, err := s.DB.Exec(ctx, `UPDATE platform_settings SET vpn_two_factor = $1 WHERE id = true`, in.Body.Enabled); err != nil {
			return nil, internalError("save vpn 2fa", err)
		}
		if err := nb.SetLocalMFA(ctx, s.vpnMFAWanted(ctx)); err != nil {
			return nil, internalError("update vpn 2fa", err)
		}
		out := &VpnTwoFactorOutput{}
		out.Body.Enabled = in.Body.Enabled
		return out, nil
	})

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
			return nil, internalError("check identity module", err)
		}
		nb, nbAvailable, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		out.Body.Available = dirAvailable && nbAvailable
		out.Body.DomainConfigured = s.domainConfigured()
		if !out.Body.Available {
			return out, nil
		}

		devices, err := nb.ListPeers(ctx)
		if err != nil {
			return nil, internalError("list connected devices", err)
		}
		out.Body.Devices = devices

		users, err := dirClient.ListUsers()
		if err != nil {
			return nil, internalError("list users", err)
		}
		rows, err := s.DB.Query(ctx, `SELECT username, COALESCE(to_char(key_expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''), nb_user_id <> '' FROM vpn_access`)
		if err != nil {
			return nil, internalError("list vpn access", err)
		}
		defer rows.Close()
		hasAccess := make(map[string]bool)
		keyExpires := make(map[string]string)
		login := make(map[string]bool)
		for rows.Next() {
			var u, exp string
			var isLogin bool
			if err := rows.Scan(&u, &exp, &isLogin); err != nil {
				return nil, internalError("scan vpn access", err)
			}
			hasAccess[u] = true
			keyExpires[u] = exp
			login[u] = isLogin
		}

		for _, u := range users {
			out.Body.Users = append(out.Body.Users, VpnUserOut{Username: u.Username, Name: u.Name, HasAccess: hasAccess[u.Username], KeyExpiresAt: keyExpires[u.Username], Login: login[u.Username]})
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
			return nil, internalError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first (or it's still finishing setup — try again in a minute)")
		}
		groupID, err := userVpnGroup(ctx, nb, in.Username, true)
		if err != nil {
			return nil, internalError("create vpn group", err)
		}
		// New access replaces the old; the old key or login must not stay valid.
		var oldKeyID, oldUserID string
		if err := s.DB.QueryRow(ctx, `SELECT setup_key_id, nb_user_id FROM vpn_access WHERE username = $1`, in.Username).Scan(&oldKeyID, &oldUserID); err == nil {
			if oldKeyID != "" {
				if err := nb.DeleteSetupKey(ctx, oldKeyID); err != nil {
					log.Printf("delete previous setup key for %s: %v", in.Username, err)
				}
			}
			if oldUserID != "" {
				if err := nb.DeleteUser(ctx, oldUserID); err != nil {
					log.Printf("delete previous vpn login for %s: %v", in.Username, err)
				}
			}
		}

		out := &EnableVpnOutput{}
		if s.vpnTwoFactor(ctx) {
			// The person proves who they are with password + authenticator
			// code, so their devices skip the "waiting for approval" step.
			approvedID, err := s.ensureAccessPolicy(ctx, nb)
			if err != nil {
				return nil, internalError("prepare access policy", err)
			}
			if err := nb.SetLocalMFA(ctx, true); err != nil {
				return nil, internalError("turn on 2fa", err)
			}
			email := in.Username + "@vpn.local"
			u, err := nb.CreateLocalUser(ctx, email, in.Username, []string{groupID, approvedID})
			if err != nil {
				return nil, internalError("create vpn login", err)
			}
			_, err = s.DB.Exec(ctx, `
				INSERT INTO vpn_access (username, setup_key_id, setup_key, key_expires_at, nb_user_id, nb_password)
				VALUES ($1, '', '', NULL, $2, $3)
				ON CONFLICT (username) DO UPDATE SET setup_key_id = '', setup_key = '', key_expires_at = NULL, nb_user_id = $2, nb_password = $3, created_at = now()
			`, in.Username, u.ID, u.Password)
			if err != nil {
				return nil, internalError("record vpn access", err)
			}
			out.Body.LoginEmail = email
			out.Body.LoginPassword = u.Password
		} else {
			key, err := nb.CreateSetupKey(ctx, in.Username, []string{groupID})
			if err != nil {
				return nil, internalError("create setup key", err)
			}
			_, err = s.DB.Exec(ctx, `
				INSERT INTO vpn_access (username, setup_key_id, setup_key, key_expires_at, nb_user_id, nb_password)
				VALUES ($1, $2, $3, now() + $4 * interval '1 second', '', '')
				ON CONFLICT (username) DO UPDATE SET setup_key_id = $2, setup_key = $3, key_expires_at = now() + $4 * interval '1 second', nb_user_id = '', nb_password = '', created_at = now()
			`, in.Username, key.ID, key.Key, int(netbird.SetupKeyLifetime.Seconds()))
			if err != nil {
				return nil, internalError("record vpn access", err)
			}
			out.Body.SetupKey = key.Key
		}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disable-vpn-user",
		Method:      "POST",
		Path:        "/api/vpn/users/{username}/disable",
		Summary:     "Revoke a company user's VPN access: the enrollment key and every device enrolled with it",
	}, func(ctx context.Context, in *VpnUsernameInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first")
		}
		if err := s.revokeVpnAccess(ctx, nb, in.Username); err != nil {
			return nil, internalError("revoke vpn access", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "approve-vpn-device",
		Method:      "POST",
		Path:        "/api/vpn/devices/{id}/approve",
		Summary:     "Let a newly enrolled device onto the VPN",
	}, func(ctx context.Context, in *VpnDeviceInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first")
		}
		groupID, err := s.ensureAccessPolicy(ctx, nb)
		if err != nil {
			return nil, internalError("find approved group", err)
		}
		if err := nb.AddPeerToGroup(ctx, groupID, in.ID); err != nil {
			return nil, internalError("approve device", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "remove-vpn-device",
		Method:      "DELETE",
		Path:        "/api/vpn/devices/{id}",
		Summary:     "Remove one enrolled device from the VPN",
	}, func(ctx context.Context, in *VpnDeviceInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		nb, available, err := s.netbirdClient(ctx)
		if err != nil {
			return nil, internalError("check vpn module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the VPN module first")
		}
		if err := nb.DeletePeer(ctx, in.ID); err != nil {
			return nil, internalError("remove device", err)
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
		var setupKey, loginUser, loginPassword string
		err := s.DB.QueryRow(ctx, `SELECT setup_key, nb_user_id, nb_password FROM vpn_access WHERE username = $1`, in.Username).Scan(&setupKey, &loginUser, &loginPassword)
		if err != nil {
			return nil, huma.Error404NotFound("no VPN access for this user yet")
		}
		// Port 8090, not the shared edge port (8000) — netbird-server's
		// client protocol is native gRPC and gets its own direct host port
		// instead of going through nginx. See proxy/nginx.go.
		managementURL := "http://" + s.Modules.Hostname("vpn-netbird", "") + ":8090"
		var text string
		if loginUser != "" {
			text = fmt.Sprintf(`Company VPN setup for %s
==============================================

1. Install the NetBird app:
   https://netbird.io/download

2. Open the app and choose "Use your own server" / advanced setup.
   Server (Management URL): %s

3. Click Connect. Your web browser opens a login page. Sign in with:
   Email:    %s
   Password: %s

4. The first time, the page shows a QR code. Scan it with the Google
   Authenticator app (or any authenticator app) on your phone, then type the
   6-digit code it shows.

From then on, signing in takes your password and the current 6-digit code
from the app. You will be asked to sign in again about once a day.
If you lose your phone, ask your admin to reset your VPN access.
`, in.Username, managementURL, in.Username+"@vpn.local", loginPassword)
		} else {
			text = fmt.Sprintf(`Company VPN setup for %s
==============================================

1. Install the NetBird app:
   https://netbird.io/download

2. Open the app and choose "Use your own server" / advanced setup.
   Server (Management URL): %s

3. When asked for a setup key, paste this:
   %s

That's it — no username or password needed, just this one key.
The key works for one device and expires after 24 hours. If it stops
working (or you need to add another device), ask your admin for a new one.
`, in.Username, managementURL, setupKey)
		}

		return &huma.StreamResponse{
			Body: func(ctx huma.Context) {
				ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
				ctx.SetHeader("Content-Disposition", fmt.Sprintf(`attachment; filename="vpn-setup-%s.txt"`, in.Username))
				ctx.BodyWriter().Write([]byte(text))
			},
		}, nil
	})
}
