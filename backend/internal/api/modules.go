package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/modules"
)

// sensitiveConfigKeyHints catches secret-looking config keys that aren't
// declared in a module's *current* manifest at all — e.g. leftovers from a
// since-removed field (vpn-netbird's old Dex integration left
// dex_client_secret/ldap_bind_password sitting in already-installed
// modules' stored config after Dex was dropped from the manifest; the
// schema-driven check below has nothing to match those against). A
// manifest can declare new sensitive fields it forgets to mark hidden, or
// a field can go stale like this — name-based matching is the backstop for
// both, on top of the precise, intentional manifest-driven redaction.
// database_url is here for the same reason management_pat is declared
// hidden in vpn-netbird's manifest: it's injected by doInstall for any
// module with needs_database: true (see modules/manifest.go), never
// something a module declares as a real ConfigSchema field, so the
// schema-driven check has nothing to match it against either — caught
// live during the wiki module's first install, where it briefly leaked
// the platform's own Postgres password over this endpoint.
var sensitiveConfigKeyHints = []string{"password", "secret", "token", "_key", "apikey", "database_url"}

func looksSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, hint := range sensitiveConfigKeyHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// redactSecretConfig strips a module's hidden/secret config values before
// they leave the backend — GET /api/modules used to return every module's
// full config map verbatim, including fields the manifest itself marks
// `hidden: true` specifically so they're never shown to the admin (internal
// RPC secrets, API tokens, the NetBird management PAT, ...). The frontend
// never actually reads live secret values from this endpoint (the install
// form only uses config_schema defaults), so this can't regress anything —
// it just closes a needless exposure of every module's crown-jewel secrets
// in one authenticated API response.
func redactSecretConfig(manifest *modules.Manifest, config map[string]string) map[string]string {
	if config == nil {
		return nil
	}
	redact := make(map[string]bool, len(manifest.ConfigSchema))
	for _, f := range manifest.ConfigSchema {
		if f.Hidden || strings.HasPrefix(f.Type, "secret") {
			redact[f.Key] = true
		}
	}
	out := make(map[string]string, len(config))
	for k, v := range config {
		if redact[k] || looksSensitive(k) {
			continue
		}
		out[k] = v
	}
	return out
}

type ListModulesInput struct {
	SessionToken string `cookie:"itp_session"`
}

type ModuleLink struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

type ListModulesOutput struct {
	Body struct {
		Catalog  []*modules.Manifest     `json:"catalog"`
		Statuses []modules.Status        `json:"statuses"`
		Links    map[string][]ModuleLink `json:"links"`
	}
}

type ModuleIDInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           string `path:"id"`
}

type InstallModuleInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           string `path:"id"`
	Body         map[string]string
}

type SetVisibilityInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           string `path:"id"`
	Body         struct {
		Visibility string `json:"visibility"` // "public" or "private"
	}
}

type ModuleActionOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}

func registerModules(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-modules",
		Method:      "GET",
		Path:        "/api/modules",
		Summary:     "List the module catalog with install status",
	}, func(ctx context.Context, in *ListModulesInput) (*ListModulesOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		statuses, err := s.Modules.ListStatuses(ctx)
		if err != nil {
			return nil, internalError("list statuses", err)
		}
		for i, st := range statuses {
			if manifest, ok := s.Registry.Get(st.ModuleID); ok {
				statuses[i].Config = redactSecretConfig(manifest, st.Config)
			}
		}

		out := &ListModulesOutput{}
		out.Body.Catalog = s.Registry.List()
		out.Body.Statuses = statuses
		out.Body.Links = make(map[string][]ModuleLink)
		for _, man := range out.Body.Catalog {
			for _, r := range man.Routes {
				out.Body.Links[man.ID] = append(out.Body.Links[man.ID], ModuleLink{
					Name:     r.Name,
					Hostname: s.Modules.Hostname(man.ID, r.Name),
				})
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "install-module",
		Method:      "POST",
		Path:        "/api/modules/{id}/install",
		Summary:     "Start installing a module in the background; poll GET /api/modules for status",
	}, func(ctx context.Context, in *InstallModuleInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.Modules.Install(ctx, in.ID, in.Body); err != nil {
			return nil, huma.Error400BadRequest("install failed", err)
		}
		if in.ID == "vpn-netbird" {
			// Runs independently of the install goroutine above: waits for
			// the module to actually come up, then does the one-time
			// dance only NetBird's own API can do (not config.yaml) —
			// bootstrap the first account and get a management token our
			// own panel uses for everything else (setup keys, peer list,
			// routes).
			go func() {
				bgCtx := context.Background()
				s.bootstrapNetbird(bgCtx)
				// Depends on bootstrapNetbird's management token, so it
				// can't run concurrently with it — chained, not a
				// separate goroutine.
				s.bootstrapInternalGateway(bgCtx)
			}()
		}
		if in.ID == "compute-mesh" {
			// Same shape as vpn-netbird's bootstrap below: wait for the
			// module to actually come up, then do the one-time setup
			// (create the device group) that has no config-time
			// equivalent — see bootstrapComputeMesh's own doc comment.
			go s.bootstrapComputeMesh(context.Background())
		}
		if in.ID == "fileshare-webdav" {
			// Every other user/group management endpoint creates a WebDAV
			// folder at the moment it creates the LDAP account/group — but
			// that only covers accounts created *after* WebDAV exists. If
			// LDAP was set up first (the normal order — see
			// ldap-openldap's hard dependency on vpn-netbird above, WebDAV
			// has no such ordering requirement), every existing user and
			// group would otherwise have working logins but no folder to
			// actually use, and no way to create one themselves (root is
			// read-only by design). Back-fill everyone already in the
			// directory once the module is actually up.
			go s.backfillWebdavFolders(context.Background())
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "enable-module",
		Method:      "POST",
		Path:        "/api/modules/{id}/enable",
		Summary:     "Start a stopped module",
	}, func(ctx context.Context, in *ModuleIDInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.Modules.Enable(ctx, in.ID); err != nil {
			return nil, huma.Error400BadRequest("enable failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disable-module",
		Method:      "POST",
		Path:        "/api/modules/{id}/disable",
		Summary:     "Stop a running module",
	}, func(ctx context.Context, in *ModuleIDInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.Modules.Disable(ctx, in.ID); err != nil {
			return nil, huma.Error400BadRequest("disable failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-module-visibility",
		Method:      "POST",
		Path:        "/api/modules/{id}/visibility",
		Summary:     "Make a module's primary route public (reachable on the internet) or private (VPN-only)",
	}, func(ctx context.Context, in *SetVisibilityInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.Modules.SetVisibility(ctx, in.ID, in.Body.Visibility); err != nil {
			return nil, huma.Error400BadRequest("set visibility failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "uninstall-module",
		Method:      "DELETE",
		Path:        "/api/modules/{id}",
		Summary:     "Stop and remove a module",
	}, func(ctx context.Context, in *ModuleIDInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.Modules.Uninstall(ctx, in.ID); err != nil {
			return nil, huma.Error400BadRequest("uninstall failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
