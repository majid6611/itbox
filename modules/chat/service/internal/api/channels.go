package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/chat/internal/directory"
)

type ListChannelsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type ListChannelsOutput struct {
	Body struct {
		// Channels are just every company group — no separate "create a
		// channel" step, and open to every employee (chat doesn't have
		// wiki's per-page permission model; that wasn't asked for here).
		Channels []string `json:"channels"`
	}
}

type ListUsersInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type ListUsersOutput struct {
	Body struct {
		Users []UserOut `json:"users"`
	}
}

func registerChannels(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-chat-channels",
		Method:      "GET",
		Path:        "/api/portal/chat/channels",
		Summary:     "List every group channel",
	}, func(ctx context.Context, in *ListChannelsInput) (*ListChannelsOutput, error) {
		if _, err := s.requireEmployeeAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		groups, err := directory.ListGroups(ctx, s.DB)
		if err != nil {
			return nil, huma.Error500InternalServerError("list groups", err)
		}
		out := &ListChannelsOutput{}
		out.Body.Channels = groups
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-chat-users",
		Method:      "GET",
		Path:        "/api/portal/chat/users",
		Summary:     "List every employee, with live online status — for starting a DM",
	}, func(ctx context.Context, in *ListUsersInput) (*ListUsersOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		usernames, err := directory.ListUsers(ctx, s.DB)
		if err != nil {
			return nil, huma.Error500InternalServerError("list users", err)
		}
		out := &ListUsersOutput{}
		for _, u := range usernames {
			if u == username {
				continue // no point DMing yourself
			}
			out.Body.Users = append(out.Body.Users, UserOut{Username: u, Online: s.Hub.IsOnline(u)})
		}
		return out, nil
	})
}
