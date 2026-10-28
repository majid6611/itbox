package api

import (
	"context"
	"it-platform/wiki/internal/dbx"
	"log"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/wiki/internal/auth"
	"it-platform/wiki/internal/directory"
	"it-platform/wiki/internal/s3client"
)

const (
	employeeSessionCookieName = "itp_employee_session"
	adminSessionCookieName    = "itp_session"
)

type Server struct {
	DB *pgxpool.Pool
}

func NewRouter(s *Server) http.Handler {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Wiki Module API", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "wiki-health",
		Method:      "GET",
		Path:        "/api/portal/wiki/health",
		Summary:     "Health check",
	}, func(ctx context.Context, in *HealthInput) (*HealthOutput, error) {
		out := &HealthOutput{}
		out.Body.OK = true
		return out, nil
	})

	registerWiki(api, s)
	registerAttachments(api, s)
	registerPermissions(api, s)

	return router
}

// internalError logs the real cause server-side and returns a generic 500
// to the client — the raw error text (a DB/LDAP/S3 driver message) can
// reveal internal infrastructure details and has no reason to leave this
// process for a failure the caller didn't cause.
func internalError(msg string, err error) huma.StatusError {
	log.Printf("%s: %v", msg, err)
	dbx.SystemLog("error", "%s: %v", msg, err)
	return huma.Error500InternalServerError(msg)
}

// requireEmployeeAuth validates the employee portal's session cookie —
// issued and tracked by core, checked here by reading the same shared
// table directly (see internal/auth).
func (s *Server) requireEmployeeAuth(ctx context.Context, sessionToken string) (string, error) {
	username, err := auth.ValidateEmployeeSession(ctx, s.DB, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return username, nil
}

// requireAdminAuth validates the admin session cookie the same way.
func (s *Server) requireAdminAuth(ctx context.Context, sessionToken string) (string, error) {
	email, err := auth.ValidateAdminSession(ctx, s.DB, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return email, nil
}

func (s *Server) employeeGroup(ctx context.Context, username string) (string, error) {
	return directory.GroupFor(ctx, s.DB, username)
}

func (s *Server) wikiS3Client(ctx context.Context) (*s3client.Client, bool, error) {
	return s3client.FromInstalledModules(ctx, s.DB)
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

	var rules []WikiPermissionRule
	for rows.Next() {
		var r WikiPermissionRule
		if err := rows.Scan(&r.Group, &r.Access); err != nil {
			return false, err
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return ruleAllows(rules, userGroup, needWrite), nil
}

// ruleAllows is the permission rule itself: a page with no rules is open to
// everyone; otherwise only a group with a rule at or above the requested
// level gets in (any rule grants read, only "write" grants write).
func ruleAllows(rules []WikiPermissionRule, userGroup string, needWrite bool) bool {
	if len(rules) == 0 {
		return true
	}
	for _, r := range rules {
		if r.Group != userGroup {
			continue
		}
		if !needWrite || r.Access == "write" {
			return true
		}
	}
	return false
}

// allRules loads every page's permission rules in one query, so listing or
// searching many pages doesn't cost one query per page.
func (s *Server) allRules(ctx context.Context) (map[int][]WikiPermissionRule, error) {
	rows, err := s.DB.Query(ctx, `SELECT page_id, group_name, access FROM wiki_permissions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]WikiPermissionRule)
	for rows.Next() {
		var id int
		var r WikiPermissionRule
		if err := rows.Scan(&id, &r.Group, &r.Access); err != nil {
			return nil, err
		}
		out[id] = append(out[id], r)
	}
	return out, rows.Err()
}

// cleanPagePath normalises a page path and rejects ones that would make a
// confusing tree: empty or "."/".." segments ("a//b", "../x"), control
// characters, or absurd lengths.
func cleanPagePath(raw string) (string, error) {
	path := strings.Trim(strings.TrimSpace(raw), "/")
	if path == "" {
		return "", huma.Error400BadRequest("page path can't be empty")
	}
	if len(path) > 300 {
		return "", huma.Error400BadRequest("page path is too long")
	}
	for _, seg := range strings.Split(path, "/") {
		if strings.TrimSpace(seg) == "" || seg == "." || seg == ".." {
			return "", huma.Error400BadRequest(`page path can't contain empty, "." or ".." parts`)
		}
		if len(seg) > 100 {
			return "", huma.Error400BadRequest("a part of the page path is too long (100 characters max)")
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return "", huma.Error400BadRequest("page path can't contain control characters")
			}
		}
	}
	return path, nil
}

func (s *Server) getPageByPath(ctx context.Context, path string) (id int, title string, ok bool, err error) {
	err = s.DB.QueryRow(ctx, `SELECT id, title FROM wiki_pages WHERE path = $1`, path).Scan(&id, &title)
	if err != nil {
		return 0, "", false, nil
	}
	return id, title, true, nil
}
