package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/voip/internal/pbx"
)

type GetSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type SettingsOutput struct {
	Body Settings
}

type SaveSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		TrunkEnabled  bool   `json:"trunk_enabled"`
		TrunkHost     string `json:"trunk_host"`
		TrunkUsername string `json:"trunk_username"`
		// Empty means "keep the one already saved" — the stored password
		// is never sent back to the browser to be re-submitted.
		TrunkPassword      string `json:"trunk_password,omitempty" required:"false"`
		TrunkCallerID      string `json:"trunk_caller_id"`
		AllowInternational bool   `json:"allow_international"`
	}
}

func (s *Server) trunkStatus(ctx context.Context, st pbx.Settings) string {
	if !st.TrunkEnabled {
		return "off"
	}
	items, err := s.AMI.List(ctx, "PJSIPShowRegistrationsOutbound", "OutboundRegistrationDetail")
	if err != nil {
		return "not_connected"
	}
	for _, it := range items {
		if it["ObjectName"] == "trunk-reg" && it["Status"] == "Registered" {
			return "connected"
		}
	}
	return "not_connected"
}

func registerSettings(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-voip-settings",
		Method:      "GET",
		Path:        "/api/voip/settings",
		Summary:     "Get the outside line (SIP trunk) settings and whether it's connected (admin only)",
	}, func(ctx context.Context, in *GetSettingsInput) (*SettingsOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		st, err := s.loadSettings(ctx)
		if err != nil {
			return nil, internalError("load settings", err)
		}
		out := &SettingsOutput{}
		out.Body = Settings{
			TrunkEnabled:       st.TrunkEnabled,
			TrunkHost:          st.TrunkHost,
			TrunkUsername:      st.TrunkUsername,
			TrunkPasswordSet:   st.TrunkPassword != "",
			TrunkCallerID:      st.TrunkCallerID,
			AllowInternational: st.AllowInternational,
			TrunkStatus:        s.trunkStatus(ctx, st),
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-voip-settings",
		Method:      "PUT",
		Path:        "/api/voip/settings",
		Summary:     "Save the outside line (SIP trunk) settings (admin only)",
	}, func(ctx context.Context, in *SaveSettingsInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		b := in.Body
		current, err := s.loadSettings(ctx)
		if err != nil {
			return nil, internalError("load settings", err)
		}
		password := current.TrunkPassword
		if b.TrunkPassword != "" {
			password = b.TrunkPassword
		}

		if b.TrunkHost != "" && !pbx.ValidTrunkHost(b.TrunkHost) {
			return nil, huma.Error400BadRequest("server must be a hostname or IP, optionally with :port")
		}
		if b.TrunkUsername != "" && !pbx.ValidTrunkUsername(b.TrunkUsername) {
			return nil, huma.Error400BadRequest("username can only contain letters, numbers and . _ + -")
		}
		if b.TrunkPassword != "" && !pbx.ValidTrunkPassword(b.TrunkPassword) {
			return nil, huma.Error400BadRequest("password can't contain spaces or ';'")
		}
		if !pbx.ValidCallerID(b.TrunkCallerID) {
			return nil, huma.Error400BadRequest("caller ID must be a phone number (digits, optional leading +)")
		}
		if b.TrunkEnabled && (b.TrunkHost == "" || b.TrunkUsername == "" || password == "") {
			return nil, huma.Error400BadRequest("server, username and password are all needed to turn the outside line on")
		}

		_, err = s.DB.Exec(ctx, `
			UPDATE voip_settings SET trunk_enabled = $1, trunk_host = $2, trunk_username = $3,
				trunk_password = $4, trunk_caller_id = $5, allow_international = $6
			WHERE id = true
		`, b.TrunkEnabled, b.TrunkHost, b.TrunkUsername, password, b.TrunkCallerID, b.AllowInternational)
		if err != nil {
			return nil, internalError("save settings", err)
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
