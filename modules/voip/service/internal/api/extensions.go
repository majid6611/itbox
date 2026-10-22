package api

import (
	"context"
	"errors"
	"regexp"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgconn"

	"it-platform/voip/internal/pbx"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type ListExtensionsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type ListExtensionsOutput struct {
	Body struct {
		PBXOnline bool `json:"pbx_online"`
		// ServerAddress is what phones register to — see serverAddress.
		ServerAddress string      `json:"server_address"`
		Extensions    []Extension `json:"extensions"`
	}
}

type CreateExtensionInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Number   string `json:"number"`
		Name     string `json:"name"`
		Username string `json:"username,omitempty" required:"false"`
	}
}

type UpdateExtensionInput struct {
	SessionToken string `cookie:"itp_session"`
	Number       string `path:"number"`
	Body         struct {
		Name        string `json:"name"`
		Username    string `json:"username,omitempty" required:"false"`
		InMainGroup bool   `json:"in_main_group"`
	}
}

type ExtensionNumberInput struct {
	SessionToken string `cookie:"itp_session"`
	Number       string `path:"number"`
}

type ExtensionOutput struct {
	Body Extension
}

func validateExtensionFields(name, username string) error {
	if !pbx.ValidName(name) {
		return huma.Error400BadRequest("name must be 1-40 letters, numbers, spaces or . ' ( ) & _ -")
	}
	if username != "" && !usernameRe.MatchString(username) {
		return huma.Error400BadRequest("invalid username")
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isUniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// deviceStates maps extension number -> "online"/"in_call"/"offline",
// from Asterisk's own view of each endpoint. ok=false if Asterisk isn't
// answering at all.
func (s *Server) deviceStates(ctx context.Context) (map[string]string, bool) {
	items, err := s.AMI.List(ctx, "PJSIPShowEndpoints", "EndpointList")
	if err != nil {
		return nil, false
	}
	states := make(map[string]string, len(items))
	for _, it := range items {
		switch it["DeviceState"] {
		case "Not in use":
			states[it["ObjectName"]] = "online"
		case "In use", "Busy", "Ringing", "Ring+Inuse", "On Hold":
			states[it["ObjectName"]] = "in_call"
		default: // "Unavailable", "Invalid", "Unknown"
			states[it["ObjectName"]] = "offline"
		}
	}
	return states, true
}

func registerExtensions(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-voip-extensions",
		Method:      "GET",
		Path:        "/api/voip/extensions",
		Summary:     "List every phone extension with its live status (admin only)",
	}, func(ctx context.Context, in *ListExtensionsInput) (*ListExtensionsOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		rows, err := s.DB.Query(ctx, `SELECT number, name, username, password, in_main_group FROM voip_extensions ORDER BY number`)
		if err != nil {
			return nil, internalError("list extensions", err)
		}
		defer rows.Close()
		out := &ListExtensionsOutput{}
		out.Body.Extensions = []Extension{}
		for rows.Next() {
			var e Extension
			if err := rows.Scan(&e.Number, &e.Name, &e.Username, &e.Password, &e.InMainGroup); err != nil {
				return nil, internalError("scan extension", err)
			}
			out.Body.Extensions = append(out.Body.Extensions, e)
		}
		if err := rows.Err(); err != nil {
			return nil, internalError("list extensions", err)
		}

		states, online := s.deviceStates(ctx)
		out.Body.PBXOnline = online
		if online {
			out.Body.ServerAddress = s.serverAddress(ctx)
		}
		for i := range out.Body.Extensions {
			e := &out.Body.Extensions[i]
			switch {
			case !online:
				e.State = "unknown"
			case states[e.Number] != "":
				e.State = states[e.Number]
			default:
				e.State = "offline"
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-voip-extension",
		Method:      "POST",
		Path:        "/api/voip/extensions",
		Summary:     "Add a phone extension — its SIP password is generated (admin only)",
	}, func(ctx context.Context, in *CreateExtensionInput) (*ExtensionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if !pbx.ValidNumber(in.Body.Number) {
			return nil, huma.Error400BadRequest("number must be 2-5 digits and can't start with 0 or 9")
		}
		if err := validateExtensionFields(in.Body.Name, in.Body.Username); err != nil {
			return nil, err
		}
		password, err := newPassword()
		if err != nil {
			return nil, internalError("generate password", err)
		}
		_, err = s.DB.Exec(ctx, `INSERT INTO voip_extensions (number, name, username, password) VALUES ($1, $2, $3, $4)`,
			in.Body.Number, in.Body.Name, nullable(in.Body.Username), password)
		if c, ok := isUniqueViolation(err); ok {
			if c == "voip_extensions_username_key" {
				return nil, huma.Error409Conflict("that person already has an extension")
			}
			return nil, huma.Error409Conflict("that number is already taken")
		}
		if err != nil {
			return nil, internalError("create extension", err)
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ExtensionOutput{}
		out.Body = Extension{Number: in.Body.Number, Name: in.Body.Name, Username: nullable(in.Body.Username), Password: password, State: "offline"}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-voip-extension",
		Method:      "PATCH",
		Path:        "/api/voip/extensions/{number}",
		Summary:     "Change an extension's name, owner, or main-number membership (admin only)",
	}, func(ctx context.Context, in *UpdateExtensionInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := validateExtensionFields(in.Body.Name, in.Body.Username); err != nil {
			return nil, err
		}
		tag, err := s.DB.Exec(ctx, `UPDATE voip_extensions SET name = $2, username = $3, in_main_group = $4 WHERE number = $1`,
			in.Number, in.Body.Name, nullable(in.Body.Username), in.Body.InMainGroup)
		if _, ok := isUniqueViolation(err); ok {
			return nil, huma.Error409Conflict("that person already has an extension")
		}
		if err != nil {
			return nil, internalError("update extension", err)
		}
		if tag.RowsAffected() == 0 {
			return nil, huma.Error404NotFound("no such extension")
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "reset-voip-extension-password",
		Method:      "POST",
		Path:        "/api/voip/extensions/{number}/reset-password",
		Summary:     "Generate a new SIP password — the phone stops working until it's re-entered (admin only)",
	}, func(ctx context.Context, in *ExtensionNumberInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		password, err := newPassword()
		if err != nil {
			return nil, internalError("generate password", err)
		}
		tag, err := s.DB.Exec(ctx, `UPDATE voip_extensions SET password = $2 WHERE number = $1`, in.Number, password)
		if err != nil {
			return nil, internalError("reset password", err)
		}
		if tag.RowsAffected() == 0 {
			return nil, huma.Error404NotFound("no such extension")
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-voip-extension",
		Method:      "DELETE",
		Path:        "/api/voip/extensions/{number}",
		Summary:     "Remove an extension (admin only)",
	}, func(ctx context.Context, in *ExtensionNumberInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		tag, err := s.DB.Exec(ctx, `DELETE FROM voip_extensions WHERE number = $1`, in.Number)
		if err != nil {
			return nil, internalError("delete extension", err)
		}
		if tag.RowsAffected() == 0 {
			return nil, huma.Error404NotFound("no such extension")
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
