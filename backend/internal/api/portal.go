package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
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

// employeeGroup looks up the current employee's single group — used by
// /api/portal/me, and by any employee-facing feature-module (wiki, later
// chat) that reads this same database directly rather than calling back
// here. "" if they're not in a group somehow (shouldn't happen — groups
// are required — but degrades to "matches nothing" rather than erroring).
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
}
