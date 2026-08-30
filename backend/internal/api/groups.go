package api

import (
	"context"
	"log"

	"github.com/danielgtaylor/huma/v2"
)

type ListGroupsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type GroupOut struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type ListGroupsOutput struct {
	Body struct {
		Available bool       `json:"available"`
		Groups    []GroupOut `json:"groups"`
	}
}

type CreateGroupInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Name string `json:"name"`
	}
}

type GroupNameInput struct {
	SessionToken string `cookie:"itp_session"`
	Name         string `path:"name"`
}

func registerGroups(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-groups",
		Method:      "GET",
		Path:        "/api/groups",
		Summary:     "List company groups (requires the Identity module)",
	}, func(ctx context.Context, in *ListGroupsInput) (*ListGroupsOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		out := &ListGroupsOutput{}
		out.Body.Available = available
		if !available {
			return out, nil
		}
		groups, err := client.ListGroups()
		if err != nil {
			return nil, internalError("list groups", err)
		}
		for _, g := range groups {
			out.Body.Groups = append(out.Body.Groups, GroupOut{Name: g.Name, Members: g.Members})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-group",
		Method:      "POST",
		Path:        "/api/groups",
		Summary:     "Create a company group",
	}, func(ctx context.Context, in *CreateGroupInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		if in.Body.Name == "" {
			return nil, huma.Error400BadRequest("group name is required")
		}
		if err := client.CreateGroup(in.Body.Name); err != nil {
			return nil, huma.Error400BadRequest("create group failed", err)
		}
		if err := s.ensureWebdavFolders(ctx, in.Body.Name); err != nil {
			log.Printf("create webdav folder for group %s: %v", in.Body.Name, err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.rebuildWebdavRules(ctx, usernames, groupOf, groupNames); err != nil {
			log.Printf("rebuild webdav rules after creating group %s: %v", in.Body.Name, err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-group",
		Method:      "DELETE",
		Path:        "/api/groups/{name}",
		Summary:     "Delete a company group",
	}, func(ctx context.Context, in *GroupNameInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		if err := client.DeleteGroup(in.Name); err != nil {
			return nil, huma.Error400BadRequest("delete group failed", err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.rebuildWebdavRules(ctx, usernames, groupOf, groupNames); err != nil {
			log.Printf("rebuild webdav rules after deleting group %s: %v", in.Name, err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
