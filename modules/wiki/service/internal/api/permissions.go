package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type ListAllWikiPagesInput struct {
	SessionToken string `cookie:"itp_session"`
}

type ListAllWikiPagesOutput struct {
	Body struct {
		Pages []WikiPageSummary `json:"pages"`
	}
}

type GetWikiPermissionsInput struct {
	SessionToken string `cookie:"itp_session"`
	Path         string `query:"path"`
}

type GetWikiPermissionsOutput struct {
	Body struct {
		Rules []WikiPermissionRule `json:"rules"`
	}
}

type SetWikiPermissionsInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Path  string               `json:"path"`
		Rules []WikiPermissionRule `json:"rules"`
	}
}

// registerPermissions is admin-only — deliberately not something an
// employee, even one with write access to a page, can change themselves.
// Keeps "who can see this" a decision the admin makes on purpose, not
// something that can be granted away accidentally.
func registerPermissions(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-all-wiki-pages",
		Method:      "GET",
		Path:        "/api/wiki/pages",
		Summary:     "List every wiki page, unfiltered by permission (admin only) — used to pick a page to manage",
	}, func(ctx context.Context, in *ListAllWikiPagesInput) (*ListAllWikiPagesOutput, error) {
		if _, err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		rows, err := s.DB.Query(ctx, `SELECT id, path, title FROM wiki_pages ORDER BY path`)
		if err != nil {
			return nil, huma.Error500InternalServerError("list pages", err)
		}
		defer rows.Close()
		out := &ListAllWikiPagesOutput{}
		for rows.Next() {
			var p WikiPageSummary
			if err := rows.Scan(&p.ID, &p.Path, &p.Title); err != nil {
				return nil, huma.Error500InternalServerError("scan page", err)
			}
			out.Body.Pages = append(out.Body.Pages, p)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-wiki-permissions",
		Method:      "GET",
		Path:        "/api/wiki/permissions",
		Summary:     "Get which groups can read/write a wiki page (admin only)",
	}, func(ctx context.Context, in *GetWikiPermissionsInput) (*GetWikiPermissionsOutput, error) {
		if _, err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		pageID, _, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		rows, err := s.DB.Query(ctx, `SELECT group_name, access FROM wiki_permissions WHERE page_id = $1 ORDER BY group_name`, pageID)
		if err != nil {
			return nil, huma.Error500InternalServerError("list permissions", err)
		}
		defer rows.Close()
		out := &GetWikiPermissionsOutput{}
		for rows.Next() {
			var r WikiPermissionRule
			if err := rows.Scan(&r.Group, &r.Access); err != nil {
				return nil, huma.Error500InternalServerError("scan permission", err)
			}
			out.Body.Rules = append(out.Body.Rules, r)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-wiki-permissions",
		Method:      "POST",
		Path:        "/api/wiki/permissions",
		Summary:     "Set which groups can read/write a wiki page — empty list means open to everyone (admin only)",
	}, func(ctx context.Context, in *SetWikiPermissionsInput) (*ActionOutput, error) {
		if _, err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		pageID, _, ok, err := s.getPageByPath(ctx, in.Body.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		for _, r := range in.Body.Rules {
			if r.Access != "read" && r.Access != "write" {
				return nil, huma.Error400BadRequest(`access must be "read" or "write"`)
			}
		}

		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("begin transaction", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `DELETE FROM wiki_permissions WHERE page_id = $1`, pageID); err != nil {
			return nil, huma.Error500InternalServerError("clear permissions", err)
		}
		for _, r := range in.Body.Rules {
			if _, err := tx.Exec(ctx, `INSERT INTO wiki_permissions (page_id, group_name, access) VALUES ($1, $2, $3)`, pageID, r.Group, r.Access); err != nil {
				return nil, huma.Error500InternalServerError("save permission", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, huma.Error500InternalServerError("commit", err)
		}

		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
