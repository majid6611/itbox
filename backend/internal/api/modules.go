package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/modules"
)

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
			return nil, huma.Error500InternalServerError("list statuses", err)
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
