package api

import (
	"context"
	"net/http"

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

func (s *Server) getPageByPath(ctx context.Context, path string) (id int, title string, ok bool, err error) {
	err = s.DB.QueryRow(ctx, `SELECT id, title FROM wiki_pages WHERE path = $1`, path).Scan(&id, &title)
	if err != nil {
		return 0, "", false, nil
	}
	return id, title, true, nil
}
