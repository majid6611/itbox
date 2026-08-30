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
		// A channel is just the employee's own LDAP group — no separate
		// "create a channel" step. Restricted to the group(s) the caller
		// actually belongs to, same as list-my-chat-groups is restricted
		// to private groups they're a member of; the read/write message
		// endpoints enforce the same boundary server-side, this just keeps
		// the sidebar from listing channels the employee couldn't use anyway.
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
		Summary:     "List the group channel(s) the employee belongs to",
	}, func(ctx context.Context, in *ListChannelsInput) (*ListChannelsOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, err := s.employeeGroup(ctx, username)
		if err != nil {
			return nil, huma.Error500InternalServerError("look up group", err)
		}
		out := &ListChannelsOutput{}
		if group != "" {
			out.Body.Channels = []string{group}
		}
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
