package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/version"
)

type GetSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type GetSettingsOutput struct {
	Body struct {
		BaseDomain      string `json:"base_domain"`
		Theme           string `json:"theme"`
		PlatformVersion string `json:"platform_version"`
	}
}

type UpdateSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		BaseDomain string `json:"base_domain"`
		Theme      string `json:"theme"`
	}
}

type GetThemeOutput struct {
	Body struct {
		Theme string `json:"theme"`
	}
}

func registerSettings(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-settings",
		Method:      "GET",
		Path:        "/api/settings",
		Summary:     "Get platform-wide settings",
	}, func(ctx context.Context, in *GetSettingsInput) (*GetSettingsOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &GetSettingsOutput{}
		out.Body.BaseDomain = s.Modules.BaseDomain()
		out.Body.Theme = s.Modules.Theme()
		out.Body.PlatformVersion = version.Version
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-settings",
		Method:      "POST",
		Path:        "/api/settings",
		Summary:     "Update platform-wide settings",
	}, func(ctx context.Context, in *UpdateSettingsInput) (*GetSettingsOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		domain := strings.ToLower(strings.TrimSpace(in.Body.BaseDomain))
		if domain == "" {
			return nil, huma.Error400BadRequest("domain can't be empty")
		}
		if strings.ContainsAny(domain, " /:\\") {
			return nil, huma.Error400BadRequest("just the domain, e.g. company.example.com — no https://, no path, no port")
		}
		if in.Body.Theme != "slate" && in.Body.Theme != "stone" {
			return nil, huma.Error400BadRequest(`theme must be "slate" or "stone"`)
		}
		if err := s.Modules.SetBaseDomain(ctx, domain); err != nil {
			return nil, internalError("save domain", err)
		}
		if err := s.Modules.SetTheme(ctx, in.Body.Theme); err != nil {
			return nil, internalError("save theme", err)
		}
		out := &GetSettingsOutput{}
		out.Body.BaseDomain = domain
		out.Body.Theme = in.Body.Theme
		out.Body.PlatformVersion = version.Version
		return out, nil
	})

	// Unauthenticated on purpose — both login screens and the employee
	// portal (no admin session) need to know the active theme before
	// there's any session to check.
	huma.Register(api, huma.Operation{
		OperationID: "get-theme",
		Method:      "GET",
		Path:        "/api/theme",
		Summary:     "Get the platform's active color theme",
	}, func(ctx context.Context, in *struct{}) (*GetThemeOutput, error) {
		out := &GetThemeOutput{}
		out.Body.Theme = s.Modules.Theme()
		return out, nil
	})
}
