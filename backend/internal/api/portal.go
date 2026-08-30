package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/s3client"
)

const employeeSessionCookieName = "itp_employee_session"

// requireEmployeeAuth validates the employee portal's own session cookie
// — separate from requireAuth's admin session by design (see
// internal/employee's package doc).
func (s *Server) requireEmployeeAuth(ctx context.Context, sessionToken string) (username string, err error) {
	if sessionToken == "" {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	username, err = s.Employee.ValidateSession(ctx, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return username, nil
}

// employeeGroup looks up the current employee's single group, for wiki
// permission checks. "" if they're not in a group somehow (shouldn't
// happen — groups are required — but permission checks degrade to
// "matches nothing" rather than erroring if it does).
func (s *Server) employeeGroup(ctx context.Context, username string) (string, error) {
	dirClient, available, err := s.directoryClient(ctx)
	if err != nil || !available {
		return "", err
	}
	groups, err := dirClient.ListGroups()
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		for _, m := range g.Members {
			if m == username {
				return g.Name, nil
			}
		}
	}
	return "", nil
}

// wikiS3Client returns a client for the s3-storage module's own bucket,
// used for wiki file attachments — same bucket the WebDAV backup uses,
// just a different key prefix ("wiki/" vs "webdav/").
func (s *Server) wikiS3Client(ctx context.Context) (*s3client.Client, bool, error) {
	status, ok, err := s.Modules.GetInstalled(ctx, "s3-storage")
	if err != nil {
		return nil, false, err
	}
	if !ok || status.Status != "running" {
		return nil, false, nil
	}
	endpoint := "http://" + s.Modules.ServiceAddr("s3-storage", "garage", 3900)
	client := s3client.New(endpoint, status.Config["access_key"], status.Config["secret_key"], status.Config["default_bucket"])
	return client, true, nil
}

// wikiPageAccess reports whether username (in userGroup) can access a
// page — true for admins always, true for anyone if the page has no
// permission rows at all (open by default), otherwise only if userGroup
// has an explicit rule at or above the requested level.
func (s *Server) wikiPageAccess(ctx context.Context, pageID int, userGroup string, isAdmin bool, needWrite bool) (bool, error) {
	if isAdmin {
		return true, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT group_name, access FROM wiki_permissions WHERE page_id = $1`, pageID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	hasAnyRule := false
	for rows.Next() {
		hasAnyRule = true
		var group, access string
		if err := rows.Scan(&group, &access); err != nil {
			return false, err
		}
		if group != userGroup {
			continue
		}
		if needWrite {
			if access == "write" {
				return true, nil
			}
			continue
		}
		return true, nil // any rule for this group grants at least read
	}
	return !hasAnyRule, nil
}

type WikiPageSummary struct {
	ID    int    `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

type WikiPage struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CanWrite  bool   `json:"can_write"`
	UpdatedAt string `json:"updated_at"`
}

type WikiRevision struct {
	ID        int    `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
}

type WikiAttachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size_bytes"`
}

// --- Employee portal auth ---

type PortalLoginInput struct {
	// Set by nginx (see proxy/nginx.go's proxy_set_header X-Real-IP) —
	// used only to key the login rate limiter below.
	ClientIP string `header:"X-Real-IP"`
	// See secureCookie's doc comment.
	ForwardedProto string `header:"X-Forwarded-Proto"`
	Body           struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
}

type PortalLoginOutput struct {
	SetCookie []string `header:"Set-Cookie"`
	Body      struct {
		Username string `json:"username"`
	}
}

type PortalLogoutInput struct {
	SessionToken   string `cookie:"itp_employee_session"`
	ForwardedProto string `header:"X-Forwarded-Proto"`
}

type PortalLogoutOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success bool `json:"success"`
	}
}

type PortalMeInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type PortalMeOutput struct {
	Body struct {
		Username string `json:"username"`
		Group    string `json:"group"`
	}
}

func registerPortal(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "portal-login",
		Method:      "POST",
		Path:        "/api/portal/login",
		Summary:     "Employee login (LDAP username/password) — separate from the admin login",
	}, func(ctx context.Context, in *PortalLoginInput) (*PortalLoginOutput, error) {
		key := rateLimitKey(in.ClientIP)
		if !s.employeeLoginLimiter.Allowed(key) {
			return nil, huma.Error429TooManyRequests("too many failed login attempts — try again later")
		}
		dirClient, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("the Identity module isn't installed yet")
		}
		token, err := s.Employee.Login(ctx, dirClient, in.Body.Username, in.Body.Password)
		if err != nil {
			s.employeeLoginLimiter.RecordFailure(key)
			return nil, huma.Error401Unauthorized("invalid username or password")
		}
		s.employeeLoginLimiter.Reset(key)
		out := &PortalLoginOutput{}
		out.Body.Username = in.Body.Username
		out.SetCookie = []string{(&http.Cookie{
			Name:     employeeSessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookie(in.ForwardedProto),
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(7 * 24 * time.Hour),
		}).String()}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "portal-logout",
		Method:      "POST",
		Path:        "/api/portal/logout",
		Summary:     "Employee logout",
	}, func(ctx context.Context, in *PortalLogoutInput) (*PortalLogoutOutput, error) {
		if in.SessionToken != "" {
			_ = s.Employee.Logout(ctx, in.SessionToken)
		}
		out := &PortalLogoutOutput{}
		out.SetCookie = (&http.Cookie{
			Name:     employeeSessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookie(in.ForwardedProto),
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Unix(0, 0),
		}).String()
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "portal-me",
		Method:      "GET",
		Path:        "/api/portal/me",
		Summary:     "Current logged-in employee",
	}, func(ctx context.Context, in *PortalMeInput) (*PortalMeOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)
		out := &PortalMeOutput{}
		out.Body.Username = username
		out.Body.Group = group
		return out, nil
	})

	registerWiki(api, s)
}

// --- Wiki ---

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

func (s *Server) getPageByPath(ctx context.Context, path string) (id int, title string, ok bool, err error) {
	err = s.DB.QueryRow(ctx, `SELECT id, title FROM wiki_pages WHERE path = $1`, path).Scan(&id, &title)
	if err != nil {
		return 0, "", false, nil
	}
	return id, title, true, nil
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
	}, func(ctx context.Context, in *DeleteWikiPageInput) (*ModuleActionOutput, error) {
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

		// Best-effort: clean up the S3 objects too, not just the DB rows —
		// otherwise every deleted page's attachments leak in the bucket
		// forever. Not fatal if storage isn't reachable; the DB delete
		// (which cascades to attachment rows) still proceeds either way.
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

		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rename-wiki-page",
		Method:      "POST",
		Path:        "/api/portal/wiki/page/rename",
		Summary:     "Change a wiki page's path (move it in the category tree) or fix a typo",
	}, func(ctx context.Context, in *RenameWikiPageInput) (*ModuleActionOutput, error) {
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

		out := &ModuleActionOutput{}
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

	registerWikiAttachments(api, s)
	registerWikiPermissions(api, s)
}
