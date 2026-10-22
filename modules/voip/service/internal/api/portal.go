package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
)

type PortalInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type MyPhone struct {
	Number   string `json:"number"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type MyPhoneOutput struct {
	Body struct {
		// Nil when the admin hasn't given this employee an extension.
		Extension *MyPhone `json:"extension"`
		// ServerAddress is what the phone registers to — see serverAddress.
		ServerAddress string `json:"server_address"`
	}
}

type DirectoryOutput struct {
	Body struct {
		Entries []DirectoryEntry `json:"entries"`
	}
}

func registerPortal(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "portal-voip-me",
		Method:      "GET",
		Path:        "/api/portal/voip/me",
		Summary:     "The logged-in employee's own extension and the details to set up their phone",
	}, func(ctx context.Context, in *PortalInput) (*MyPhoneOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		out := &MyPhoneOutput{}
		var p MyPhone
		err = s.DB.QueryRow(ctx, `SELECT number, name, password FROM voip_extensions WHERE username = $1`, username).
			Scan(&p.Number, &p.Name, &p.Password)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		if err != nil {
			return nil, internalError("load extension", err)
		}
		out.Body.Extension = &p
		out.Body.ServerAddress = s.serverAddress(ctx)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "portal-voip-directory",
		Method:      "GET",
		Path:        "/api/portal/voip/directory",
		Summary:     "Everyone's extension number, for looking up who to call",
	}, func(ctx context.Context, in *PortalInput) (*DirectoryOutput, error) {
		if _, err := s.requireEmployeeAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		rows, err := s.DB.Query(ctx, `SELECT number, name FROM voip_extensions ORDER BY name`)
		if err != nil {
			return nil, internalError("list directory", err)
		}
		defer rows.Close()
		out := &DirectoryOutput{}
		out.Body.Entries = []DirectoryEntry{}
		for rows.Next() {
			var e DirectoryEntry
			if err := rows.Scan(&e.Number, &e.Name); err != nil {
				return nil, internalError("scan directory", err)
			}
			out.Body.Entries = append(out.Body.Entries, e)
		}
		return out, rows.Err()
	})
}
