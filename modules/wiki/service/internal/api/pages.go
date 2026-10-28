package api

import (
	"context"
	"errors"
	"html"
	"strings"
	"time"
	"unicode"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		// BaseRevision is the revision the editor started from. 0 means
		// "create a new page" (refused if the path is taken); a number
		// means "edit that version" (refused if the page has moved on).
		// Left out, the save is unconditional.
		BaseRevision *int `json:"base_revision,omitempty"`
	}
}

type SaveWikiPageOutput struct {
	Body struct {
		Success    bool `json:"success"`
		ID         int  `json:"id"`
		RevisionID int  `json:"revision_id"`
	}
}

type RecentWikiInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type RecentWikiOutput struct {
	Body struct {
		Pages []WikiRecent `json:"pages"`
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

		rules, err := s.allRules(ctx)
		if err != nil {
			return nil, internalError("check access", err)
		}
		rows, err := s.DB.Query(ctx, `SELECT id, path, title FROM wiki_pages ORDER BY path`)
		if err != nil {
			return nil, internalError("list pages", err)
		}
		defer rows.Close()

		out := &ListWikiPagesOutput{}
		for rows.Next() {
			var p WikiPageSummary
			if err := rows.Scan(&p.ID, &p.Path, &p.Title); err != nil {
				return nil, internalError("scan page", err)
			}
			if ruleAllows(rules[p.ID], group, false) {
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
			return nil, internalError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, internalError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, internalError("check access", err)
		}

		var content, author string
		var revisionID int
		var updatedAt time.Time
		err = s.DB.QueryRow(ctx, `
			SELECT id, content, author, created_at FROM wiki_revisions WHERE page_id = $1 ORDER BY id DESC LIMIT 1
		`, id).Scan(&revisionID, &content, &author, &updatedAt)
		if err != nil {
			return nil, internalError("load content", err)
		}

		out := &GetWikiPageOutput{}
		out.Body = WikiPage{
			ID: id, Path: in.Path, Title: title, Content: content, CanWrite: canWrite,
			UpdatedAt: updatedAt.Format(time.RFC3339), UpdatedBy: author, RevisionID: revisionID,
		}
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
		path, err := cleanPagePath(in.Body.Path)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(in.Body.Title)
		if title == "" {
			return nil, huma.Error400BadRequest("page title can't be empty")
		}
		if len([]rune(title)) > 200 {
			return nil, huma.Error400BadRequest("page title is too long (200 characters max)")
		}
		group, _ := s.employeeGroup(ctx, username)
		content := sanitizeWikiContent(in.Body.Content)

		// One transaction with the page row locked, so two saves of the same
		// page can't both pass the "is my base still current?" check.
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, internalError("begin transaction", err)
		}
		defer tx.Rollback(ctx)

		out := &SaveWikiPageOutput{}
		var id int
		err = tx.QueryRow(ctx, `SELECT id FROM wiki_pages WHERE path = $1 FOR UPDATE`, path).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if in.Body.BaseRevision != nil && *in.Body.BaseRevision != 0 {
				return nil, huma.Error409Conflict("this page no longer exists — it was deleted or moved")
			}
			// New pages are open to create — matches "no config needed
			// unless you want to restrict something": there's nothing to
			// check permissions against yet, since the page doesn't exist.
			err = tx.QueryRow(ctx, `INSERT INTO wiki_pages (path, title) VALUES ($1, $2) RETURNING id`, path, title).Scan(&id)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					return nil, huma.Error409Conflict("a page already exists at " + path)
				}
				return nil, internalError("create page", err)
			}
			err = tx.QueryRow(ctx, `INSERT INTO wiki_revisions (page_id, content, author) VALUES ($1, $2, $3) RETURNING id`, id, content, username).Scan(&out.Body.RevisionID)
			if err != nil {
				return nil, internalError("save revision", err)
			}
		case err != nil:
			return nil, internalError("load page", err)
		default:
			canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
			if err != nil {
				return nil, internalError("check access", err)
			}
			if !canWrite {
				return nil, huma.Error403Forbidden("you don't have write access to this page")
			}
			var latestID int
			var latestContent, oldTitle string
			err = tx.QueryRow(ctx, `
				SELECT r.id, r.content, p.title FROM wiki_revisions r JOIN wiki_pages p ON p.id = r.page_id
				WHERE r.page_id = $1 ORDER BY r.id DESC LIMIT 1`, id).Scan(&latestID, &latestContent, &oldTitle)
			if err != nil {
				return nil, internalError("load content", err)
			}
			if base := in.Body.BaseRevision; base != nil {
				if *base == 0 {
					return nil, huma.Error409Conflict("a page already exists at " + path)
				}
				if *base != latestID {
					return nil, huma.Error409Conflict("someone else changed this page while you were editing")
				}
			}
			out.Body.RevisionID = latestID
			if title != oldTitle {
				if _, err := tx.Exec(ctx, `UPDATE wiki_pages SET title = $2, updated_at = now() WHERE id = $1`, id, title); err != nil {
					return nil, internalError("update page", err)
				}
			}
			// Saving without changing the text doesn't add a revision —
			// otherwise History fills with identical entries.
			if content != latestContent {
				err = tx.QueryRow(ctx, `INSERT INTO wiki_revisions (page_id, content, author) VALUES ($1, $2, $3) RETURNING id`, id, content, username).Scan(&out.Body.RevisionID)
				if err != nil {
					return nil, internalError("save revision", err)
				}
				if _, err := tx.Exec(ctx, `UPDATE wiki_pages SET updated_at = now() WHERE id = $1`, id); err != nil {
					return nil, internalError("update page", err)
				}
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, internalError("commit", err)
		}

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
			return nil, internalError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, internalError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}

		rows, err := s.DB.Query(ctx, `SELECT id, author, created_at FROM wiki_revisions WHERE page_id = $1 ORDER BY created_at DESC`, id)
		if err != nil {
			return nil, internalError("list revisions", err)
		}
		defer rows.Close()
		out := &ListWikiRevisionsOutput{}
		for rows.Next() {
			var r WikiRevision
			var createdAt time.Time
			if err := rows.Scan(&r.ID, &r.Author, &createdAt); err != nil {
				return nil, internalError("scan revision", err)
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
			return nil, internalError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, id, group, false, false)
		if err != nil {
			return nil, internalError("check access", err)
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
			return nil, internalError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, internalError("check access", err)
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
			return nil, internalError("delete page", err)
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
		newPath, err := cleanPagePath(in.Body.NewPath)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		id, _, ok, err := s.getPageByPath(ctx, in.Body.OldPath)
		if err != nil {
			return nil, internalError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canWrite, err := s.wikiPageAccess(ctx, id, group, false, true)
		if err != nil {
			return nil, internalError("check access", err)
		}
		if !canWrite {
			return nil, huma.Error403Forbidden("you don't have write access to this page")
		}

		if _, _, exists, err := s.getPageByPath(ctx, newPath); err != nil {
			return nil, internalError("check target path", err)
		} else if exists {
			return nil, huma.Error400BadRequest("a page already exists at " + newPath)
		}

		if _, err := s.DB.Exec(ctx, `UPDATE wiki_pages SET path = $2, updated_at = now() WHERE id = $1`, id, newPath); err != nil {
			return nil, internalError("rename page", err)
		}

		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "search-wiki",
		Method:      "GET",
		Path:        "/api/portal/wiki/search",
		Summary:     "Search wiki page titles and text, best matches first, filtered to what the employee can read",
	}, func(ctx context.Context, in *SearchWikiInput) (*SearchWikiOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		out := &SearchWikiOutput{}
		tsq := prefixQuery(in.Q)
		if tsq == "" {
			return out, nil
		}
		group, _ := s.employeeGroup(ctx, username)
		rules, err := s.allRules(ctx)
		if err != nil {
			return nil, internalError("check access", err)
		}

		// Matches against the page's text with the HTML tags stripped, not
		// the raw HTML (otherwise "strong" or "href" matches every page).
		// Title hits outrank body hits; every word matches as a prefix, so
		// "onboard" finds "onboarding".
		rows, err := s.DB.Query(ctx, `
			WITH cur AS (
				SELECT p.id, p.path, p.title,
				       regexp_replace(r.content, '<[^>]+>', ' ', 'g') AS body
				FROM wiki_pages p
				JOIN LATERAL (
					SELECT content FROM wiki_revisions WHERE page_id = p.id ORDER BY id DESC LIMIT 1
				) r ON true
			), q AS (SELECT to_tsquery('simple', $1) AS tsq)
			SELECT cur.id, cur.path, cur.title,
			       ts_headline('simple', cur.body, q.tsq, 'StartSel=<<, StopSel=>>, MaxFragments=1, MinWords=8, MaxWords=24')
			FROM cur, q
			WHERE (setweight(to_tsvector('simple', cur.title), 'A') || setweight(to_tsvector('simple', cur.body), 'B')) @@ q.tsq
			ORDER BY ts_rank(setweight(to_tsvector('simple', cur.title), 'A') || setweight(to_tsvector('simple', cur.body), 'B'), q.tsq) DESC, cur.path
			LIMIT 100
		`, tsq)
		if err != nil {
			return nil, internalError("search", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p WikiPageSummary
			if err := rows.Scan(&p.ID, &p.Path, &p.Title, &p.Snippet); err != nil {
				return nil, internalError("scan page", err)
			}
			p.Snippet = cleanSnippet(p.Snippet)
			if ruleAllows(rules[p.ID], group, false) {
				out.Body.Pages = append(out.Body.Pages, p)
				if len(out.Body.Pages) == 30 {
					break
				}
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "recent-wiki-changes",
		Method:      "GET",
		Path:        "/api/portal/wiki/recent",
		Summary:     "The most recently edited pages the employee can read",
	}, func(ctx context.Context, in *RecentWikiInput) (*RecentWikiOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)
		rules, err := s.allRules(ctx)
		if err != nil {
			return nil, internalError("check access", err)
		}
		rows, err := s.DB.Query(ctx, `
			SELECT p.id, p.path, p.title, r.author, r.created_at
			FROM wiki_pages p
			JOIN LATERAL (
				SELECT author, created_at FROM wiki_revisions WHERE page_id = p.id ORDER BY id DESC LIMIT 1
			) r ON true
			ORDER BY r.created_at DESC LIMIT 100`)
		if err != nil {
			return nil, internalError("recent changes", err)
		}
		defer rows.Close()
		out := &RecentWikiOutput{}
		for rows.Next() {
			var p WikiRecent
			var at time.Time
			if err := rows.Scan(&p.ID, &p.Path, &p.Title, &p.Author, &at); err != nil {
				return nil, internalError("scan page", err)
			}
			p.UpdatedAt = at.Format(time.RFC3339)
			if ruleAllows(rules[p.ID], group, false) {
				out.Body.Pages = append(out.Body.Pages, p)
				if len(out.Body.Pages) == 10 {
					break
				}
			}
		}
		return out, nil
	})
}

// prefixQuery turns what someone typed into a to_tsquery string where every
// word must match as a prefix: "onboard guide" -> "onboard:* & guide:*".
// Anything that isn't a letter or digit is dropped from each word, so the
// text can never be read as tsquery syntax.
func prefixQuery(q string) string {
	var terms []string
	for _, word := range strings.Fields(q) {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, word)
		if clean != "" {
			terms = append(terms, clean+":*")
		}
	}
	return strings.Join(terms, " & ")
}

// cleanSnippet turns the stored HTML text of a search snippet back into
// plain text (entities like &amp; decoded) and tidies the whitespace.
func cleanSnippet(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}
