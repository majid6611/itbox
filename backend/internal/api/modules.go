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
		// vpn-netbird's Dex sidecar needs the LDAP bind password/base DN
		// to search the directory — that lives in a different module's
		// stored config, which the generic install path has no way to
		// see. This is the one module with a hard (not soft) dependency
		// on ldap-openldap: without it, there's nothing for VPN logins
		// to authenticate against, so we fail fast here instead of
		// installing a VPN nobody can log into.
		if in.ID == "vpn-netbird" {
			ldapStatus, ok, err := s.Modules.GetInstalled(ctx, "ldap-openldap")
			if err != nil {
				return nil, huma.Error500InternalServerError("check identity module", err)
			}
			if !ok || ldapStatus.Status != "running" {
				return nil, huma.Error400BadRequest("install the Identity (OpenLDAP) module first — VPN logins authenticate against it")
			}
			if in.Body == nil {
				in.Body = map[string]string{}
			}
			in.Body["ldap_base_dn"] = ldapStatus.Config["base_dn"]
			in.Body["ldap_bind_password"] = ldapStatus.Config["admin_password"]
		}
		if err := s.Modules.Install(ctx, in.ID, in.Body); err != nil {
			return nil, huma.Error400BadRequest("install failed", err)
		}
		if in.ID == "vpn-netbird" {
			// Runs independently of the install goroutine above: waits for
			// the module to actually come up, then does the one-time
			// dance only NetBird's own API can do (not config.yaml) —
			// bootstrap the first account, get a management token, and
			// register our Dex sidecar as the login connector.
			go s.bootstrapNetbird(context.Background())
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
