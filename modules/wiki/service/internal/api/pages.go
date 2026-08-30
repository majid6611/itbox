package api

import (
	"context"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type ListWikiPagesInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type ListWikiPagesOutput struct {
	Body struct {
		Pages []WikiPageSummary `json:"pages"`
	}
}

type GetWikiPageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Path         string `query:"path"`
}

type GetWikiPageOutput struct {
	Body WikiPage
}

type SaveWikiPageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         struct {
		Path    string `json:"path"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
}

type SaveWikiPageOutput struct {
	Body struct {
		Success bool `json:"success"`
		ID      int  `json:"id"`
	}
}

type ListWikiRevisionsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Path         string `query:"path"`
}

type ListWikiRevisionsOutput struct {
	Body struct {
		Revisions []WikiRevision `json:"revisions"`
	}
}

type GetWikiRevisionInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Path         string `query:"path"`
	ID           int    `query:"id"`
}

type GetWikiRevisionOutput struct {
	Body struct {
		Content string `json:"content"`
	}
}

type DeleteWikiPageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Path         string `query:"path"`
}

type RenameWikiPageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
}

type SearchWikiInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Q            string `query:"q"`
}

type SearchWikiOutput struct {
	Body struct {
		Pages []WikiPageSummary `json:"pages"`
	}
}

func registerWiki(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-wiki-pages",
		Method:      "GET",
		Path:        "/api/portal/wiki/pages",
		Summary:     "List every wiki page the current employee can read",
	}, func(ctx context.Context, in *ListWikiPagesInput) (*ListWikiPagesOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		rows, err := s.DB.Query(ctx, `SELECT id, path, title FROM wiki_pages ORDER BY path`)
		if err != nil {
			return nil, huma.Error500InternalServerError("list pages", err)
		}
		defer rows.Close()

		out := &ListWikiPagesOutput{}
		for rows.Next() {
			var p WikiPageSummary
			if err := rows.Scan(&p.ID, &p.Path, &p.Title); err != nil {
				return nil, huma.Error500InternalServerError("scan page", err)
			}
			canRead, err := s.wikiPageAccess(ctx, p.ID, group, false, false)
			if err != nil {
				return nil, huma.Error500InternalServerError("check access", err)
			}
			if canRead {
				out.Body.Pages = append(out.Body.Pages, p)
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-wiki-page",
		Method:      "GET",
		Path:        "/api/portal/wiki/page",
		Summary:     "Get a wiki page's current content",
	}, func(ctx context.Context, in *GetWikiPageInput) (*GetWikiPageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		id, title, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}

		var content string
		var updatedAt time.Time
		err = s.DB.QueryRow(ctx, `
			SELECT content, created_at FROM wiki_revisions WHERE page_id = $1 ORDER BY created_at DESC LIMIT 1
		`, id).Scan(&content, &updatedAt)
		if err != nil {
			return nil, huma.Error500InternalServerError("load content", err)
		}

		out := &GetWikiPageOutput{}
		out.Body = WikiPage{ID: id, Path: in.Path, Title: title, Content: content, CanWrite: canWrite, UpdatedAt: updatedAt.Format(time.RFC3339)}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-wiki-page",
		Method:      "POST",
		Path:        "/api/portal/wiki/page",
		Summary:     "Create a page, or save a new revision of an existing one",
	}, func(ctx context.Context, in *SaveWikiPageInput) (*SaveWikiPageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		path := strings.Trim(strings.TrimSpace(in.Body.Path), "/")
		if path == "" {
			return nil, huma.Error400BadRequest("page path can't be empty")
		}
		if in.Body.Title == "" {
			return nil, huma.Error400BadRequest("page title can't be empty")
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, exists, err := s.getPageByPath(ctx, path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if exists {
			canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
			if err != nil {
				return nil, huma.Error500InternalServerError("check access", err)
			}
			if !canWrite {
				return nil, huma.Error403Forbidden("you don't have write access to this page")
			}
			if _, err := s.DB.Exec(ctx, `UPDATE wiki_pages SET title = $2, updated_at = now() WHERE id = $1`, id, in.Body.Title); err != nil {
				return nil, huma.Error500InternalServerError("update page", err)
			}
		} else {
			// New pages are open to create — matches "no config needed
			// unless you want to restrict something": there's nothing to
			// check permissions against yet, since the page doesn't exist.
			err = s.DB.QueryRow(ctx, `INSERT INTO wiki_pages (path, title) VALUES ($1, $2) RETURNING id`, path, in.Body.Title).Scan(&id)
			if err != nil {
				return nil, huma.Error500InternalServerError("create page", err)
			}
		}

		if _, err := s.DB.Exec(ctx, `INSERT INTO wiki_revisions (page_id, content, author) VALUES ($1, $2, $3)`, id, in.Body.Content, username); err != nil {
			return nil, huma.Error500InternalServerError("save revision", err)
		}

		out := &SaveWikiPageOutput{}
		out.Body.Success = true
		out.Body.ID = id
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-wiki-revisions",
		Method:      "GET",
		Path:        "/api/portal/wiki/page/revisions",
		Summary:     "List a page's change history",
	}, func(ctx context.Context, in *ListWikiRevisionsInput) (*ListWikiRevisionsOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}

		rows, err := s.DB.Query(ctx, `SELECT id, author, created_at FROM wiki_revisions WHERE page_id = $1 ORDER BY created_at DESC`, id)
		if err != nil {
			return nil, huma.Error500InternalServerError("list revisions", err)
		}
		defer rows.Close()
		out := &ListWikiRevisionsOutput{}
		for rows.Next() {
			var r WikiRevision
			var createdAt time.Time
			if err := rows.Scan(&r.ID, &r.Author, &createdAt); err != nil {
				return nil, huma.Error500InternalServerError("scan revision", err)
			}
			r.CreatedAt = createdAt.Format(time.RFC3339)
			out.Body.Revisions = append(out.Body.Revisions, r)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-wiki-revision",
		Method:      "GET",
		Path:        "/api/portal/wiki/page/revision",
		Summary:     "Get one historical revision's content",
	}, func(ctx context.Context, in *GetWikiRevisionInput) (*GetWikiRevisionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}

		var content string
		err = s.DB.QueryRow(ctx, `SELECT content FROM wiki_revisions WHERE id = $1 AND page_id = $2`, in.ID, id).Scan(&content)
		if err != nil {
			return nil, huma.Error404NotFound("no such revision")
		}
		out := &GetWikiRevisionOutput{}
		out.Body.Content = content
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-wiki-page",
		Method:      "DELETE",
		Path:        "/api/portal/wiki/page",
		Summary:     "Delete a wiki page, its history, and its attachments",
	}, func(ctx context.Context, in *DeleteWikiPageInput) (*ActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canWrite {
			return nil, huma.Error403Forbidden("you don't have write access to this page")
		}

		if s3, available, err := s.wikiS3Client(ctx); err == nil && available {
			rows, err := s.DB.Query(ctx, `SELECT s3_key FROM wiki_attachments WHERE page_id = $1`, id)
			if err == nil {
				var keys []string
				for rows.Next() {
					var key string
					if rows.Scan(&key) == nil {
						keys = append(keys, key)
					}
				}
				rows.Close()
				for _, key := range keys {
					_ = s3.Delete(ctx, key)
				}
			}
		}

		if _, err := s.DB.Exec(ctx, `DELETE FROM wiki_pages WHERE id = $1`, id); err != nil {
			return nil, huma.Error500InternalServerError("delete page", err)
		}

		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rename-wiki-page",
		Method:      "POST",
		Path:        "/api/portal/wiki/page/rename",
		Summary:     "Change a wiki page's path (move it in the category tree) or fix a typo",
	}, func(ctx context.Context, in *RenameWikiPageInput) (*ActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		newPath := strings.Trim(strings.TrimSpace(in.Body.NewPath), "/")
		if newPath == "" {
			return nil, huma.Error400BadRequest("new path can't be empty")
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, ok, err := s.getPageByPath(ctx, in.Body.OldPath)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canWrite {
			return nil, huma.Error403Forbidden("you don't have write access to this page")
		}

		if _, _, exists, err := s.getPageByPath(ctx, newPath); err != nil {
			return nil, huma.Error500InternalServerError("check target path", err)
		} else if exists {
			return nil, huma.Error400BadRequest("a page already exists at " + newPath)
		}

		if _, err := s.DB.Exec(ctx, `UPDATE wiki_pages SET path = $2, updated_at = now() WHERE id = $1`, id, newPath); err != nil {
			return nil, huma.Error500InternalServerError("rename page", err)
		}

		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "search-wiki",
		Method:      "GET",
		Path:        "/api/portal/wiki/search",
		Summary:     "Search wiki page titles and content, filtered to what the employee can read",
	}, func(ctx context.Context, in *SearchWikiInput) (*SearchWikiOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		out := &SearchWikiOutput{}
		q := strings.TrimSpace(in.Q)
		if q == "" {
			return out, nil
		}
		group, _ := s.employeeGroup(ctx, username)

		rows, err := s.DB.Query(ctx, `
			SELECT DISTINCT p.id, p.path, p.title
			FROM wiki_pages p
			JOIN LATERAL (
				SELECT content FROM wiki_revisions WHERE page_id = p.id ORDER BY created_at DESC LIMIT 1
			) r ON true
			WHERE p.title ILIKE '%' || $1 || '%' OR r.content ILIKE '%' || $1 || '%'
			ORDER BY p.path
		`, q)
		if err != nil {
			return nil, huma.Error500InternalServerError("search", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p WikiPageSummary
			if err := rows.Scan(&p.ID, &p.Path, &p.Title); err != nil {
				return nil, huma.Error500InternalServerError("scan page", err)
			}
			canRead, err := s.wikiPageAccess(ctx, p.ID, group, false, false)
			if err != nil {
				return nil, huma.Error500InternalServerError("check access", err)
			}
			if canRead {
				out.Body.Pages = append(out.Body.Pages, p)
			}
		}
		return out, nil
	})
}
