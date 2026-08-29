package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type GetSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type GetSettingsOutput struct {
	Body struct {
		BaseDomain string `json:"base_domain"`
	}
}

type UpdateSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		BaseDomain string `json:"base_domain"`
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
		if err := s.Modules.SetBaseDomain(ctx, domain); err != nil {
			return nil, huma.Error500InternalServerError("save domain", err)
		}
		out := &GetSettingsOutput{}
		out.Body.BaseDomain = domain
		return out, nil
	})
}
