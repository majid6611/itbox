package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/chat/internal/directory"
	"it-platform/chat/internal/hub"
)

type CustomGroupOut struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	CreatedBy string   `json:"created_by"`
	Members   []string `json:"members"`
}

type ListMyGroupsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type ListMyGroupsOutput struct {
	Body struct {
		Groups []CustomGroupOut `json:"groups"`
	}
}

type CreateGroupInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         struct {
		Name    string   `json:"name"`
		Members []string `json:"members"` // in addition to the creator, who's always added
	}
}

type CreateGroupOutput struct {
	Body CustomGroupOut
}

type AddMemberInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	ID           int64  `path:"id"`
	Body         struct {
		Username string `json:"username"`
	}
}

func registerCustomGroups(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-my-chat-groups",
		Method:      "GET",
		Path:        "/api/portal/chat/groups",
		Summary:     "List private groups the employee is a member of — invisible to anyone else",
	}, func(ctx context.Context, in *ListMyGroupsInput) (*ListMyGroupsOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		groups, err := s.myCustomGroups(ctx, username)
		if err != nil {
			return nil, huma.Error500InternalServerError("list groups", err)
		}
		out := &ListMyGroupsOutput{}
		out.Body.Groups = groups
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-chat-group",
		Method:      "POST",
		Path:        "/api/portal/chat/groups",
		Summary:     "Create a private group with the given members — hidden from everyone else",
	}, func(ctx context.Context, in *CreateGroupInput) (*CreateGroupOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, huma.Error400BadRequest("group name can't be empty")
		}
		realUsers, err := s.realUsernames(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check directory", err)
		}
		for _, m := range in.Body.Members {
			if m = strings.TrimSpace(m); m != "" && !realUsers[m] {
				return nil, huma.Error400BadRequest("no such employee: " + m)
			}
		}

		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("begin transaction", err)
		}
		defer tx.Rollback(ctx)

		var groupID int64
		if err := tx.QueryRow(ctx, `INSERT INTO chat_custom_groups (name, created_by) VALUES ($1, $2) RETURNING id`, name, username).Scan(&groupID); err != nil {
			return nil, huma.Error500InternalServerError("create group", err)
		}
		members := map[string]bool{username: true} // creator is always a member
		for _, m := range in.Body.Members {
			m = strings.TrimSpace(m)
			if m != "" {
				members[m] = true
			}
		}
		for m := range members {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_group_members (group_id, username) VALUES ($1, $2)`, groupID, m); err != nil {
				return nil, huma.Error500InternalServerError("add member", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, huma.Error500InternalServerError("commit", err)
		}

		memberList := make([]string, 0, len(members))
		var others []string
		for m := range members {
			memberList = append(memberList, m)
			if m != username {
				others = append(others, m)
			}
		}
		// The creator already has the group from this call's own response
		// — this is for everyone else added at creation time, so their
		// sidebar picks it up live instead of needing a reload.
		s.Hub.SendTo(others, hub.Event{Type: "group_invite", Group: &hub.GroupInvite{ID: groupID, Name: name}})

		out := &CreateGroupOutput{Body: CustomGroupOut{ID: groupID, Name: name, CreatedBy: username, Members: memberList}}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "add-chat-group-member",
		Method:      "POST",
		Path:        "/api/portal/chat/groups/{id}/members",
		Summary:     "Add someone to a private group — only existing members can",
	}, func(ctx context.Context, in *AddMemberInput) (*ActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		newMember := strings.TrimSpace(in.Body.Username)
		if newMember == "" {
			return nil, huma.Error400BadRequest("username can't be empty")
		}
		realUsers, err := s.realUsernames(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check directory", err)
		}
		if !realUsers[newMember] {
			return nil, huma.Error400BadRequest("no such employee: " + newMember)
		}
		isMember, err := s.isGroupMember(ctx, in.ID, username)
		if err != nil {
			return nil, huma.Error500InternalServerError("check membership", err)
		}
		if !isMember {
			return nil, huma.Error403Forbidden("you're not a member of this group")
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO chat_group_members (group_id, username) VALUES ($1, $2) ON CONFLICT DO NOTHING`, in.ID, newMember); err != nil {
			return nil, huma.Error500InternalServerError("add member", err)
		}
		var groupName string
		if err := s.DB.QueryRow(ctx, `SELECT name FROM chat_custom_groups WHERE id = $1`, in.ID).Scan(&groupName); err == nil {
			s.Hub.SendTo([]string{newMember}, hub.Event{Type: "group_invite", Group: &hub.GroupInvite{ID: in.ID, Name: groupName}})
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}

// realUsernames backstops the frontend's own picker — that only stops a
// well-behaved client, not a raw API call — so a private group's member
// list can't be seeded with usernames that were never real employees
// (previously possible, and previously actually happened during testing).
func (s *Server) realUsernames(ctx context.Context) (map[string]bool, error) {
	usernames, err := directory.ListUsers(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(usernames))
	for _, u := range usernames {
		set[u] = true
	}
	return set, nil
}

func (s *Server) myCustomGroups(ctx context.Context, username string) ([]CustomGroupOut, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT g.id, g.name, g.created_by
		FROM chat_custom_groups g
		JOIN chat_group_members m ON m.group_id = g.id AND m.username = $1
		ORDER BY g.id
	`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []CustomGroupOut
	for rows.Next() {
		var g CustomGroupOut
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedBy); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range groups {
		members, err := s.groupMembers(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}
		groups[i].Members = members
	}
	return groups, nil
}

func (s *Server) groupMembers(ctx context.Context, groupID int64) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT username FROM chat_group_members WHERE group_id = $1 ORDER BY username`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		members = append(members, u)
	}
	return members, rows.Err()
}

func (s *Server) isGroupMember(ctx context.Context, groupID int64, username string) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_group_members WHERE group_id = $1 AND username = $2)`, groupID, username).Scan(&exists)
	return exists, err
}
