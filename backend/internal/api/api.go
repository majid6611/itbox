package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/auth"
	"it-platform/backend/internal/docker"
	"it-platform/backend/internal/employee"
	"it-platform/backend/internal/modules"
	"it-platform/backend/internal/ratelimit"
)

type Server struct {
	Auth     *auth.Service
	Employee *employee.Service
	Docker   *docker.Client
	Modules  *modules.Manager
	Registry *modules.Registry
	DB       *pgxpool.Pool

	// GatewayContainerName is the internal VPN gateway's fixed container
	// name (see docker-compose.yaml's internal-gateway service) — needed
	// to `docker exec` its one-time NetBird enrollment.
	GatewayContainerName string

	// Separate limiters for the two login endpoints — kept apart so a
	// flood of failed employee logins (a much larger, less trusted
	// population) can never eat into the admin login's own budget.
	adminLoginLimiter    *ratelimit.Limiter
	employeeLoginLimiter *ratelimit.Limiter
}

const sessionCookieName = "itp_session"

// loginRateLimit is shared by both login endpoints — 10 failed attempts
// per IP in a 15-minute window. Generous enough that a real person
// mistyping their password a few times never gets caught by it, tight
// enough to make brute-forcing either a bcrypt-hashed admin password or an
// LDAP one impractical.
const (
	loginRateLimitMax    = 10
	loginRateLimitWindow = 15 * time.Minute
)

func NewRouter(s *Server) http.Handler {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("IT Platform API", "0.1.0"))

	s.adminLoginLimiter = ratelimit.New(loginRateLimitMax, loginRateLimitWindow)
	s.employeeLoginLimiter = ratelimit.New(loginRateLimitMax, loginRateLimitWindow)

	registerHealth(api, s)
	registerAuth(api, s)
	registerModules(api, s)
	registerUsers(api, s)
	registerGroups(api, s)
	registerVpn(api, s)
	registerSettings(api, s)
	registerBackup(api, s)
	registerComputeMesh(api, s)
	registerPortal(api, s)

	s.startBackupScheduler(context.Background())

	return router
}

// secureCookie reports whether a Set-Cookie response should carry the
// Secure attribute. Nothing in this platform terminates TLS today (nginx
// listens on plain HTTP — see docker-compose.yaml/nginx/), so this is
// always false right now, which is correct: a browser silently refuses to
// even store a Secure cookie sent over plain HTTP, so hardcoding Secure:
// true today would just break every login. If TLS is ever added in front
// of nginx, whatever terminates it needs to set X-Forwarded-Proto: https
// on the way in (standard practice) for this to start actually applying
// the protection — see also proto binding on the login/logout inputs.
func secureCookie(forwardedProto string) bool {
	return forwardedProto == "https"
}

// rateLimitKey turns the X-Real-IP nginx sets (see proxy/nginx.go) into a
// rate-limiter key, falling back to a shared bucket if it's ever missing
// (e.g. a direct request that bypassed nginx) — degrades to one shared
// limit rather than silently disabling rate limiting altogether.
func rateLimitKey(clientIP string) string {
	if clientIP == "" {
		return "unknown"
	}
	return clientIP
}

// internalError logs the real cause server-side and returns a generic 500
// to the client — the raw error text (a DB/LDAP/Docker driver message) can
// reveal internal infrastructure details and has no reason to leave this
// process for a failure the caller didn't cause. Use this instead of
// huma.Error500InternalServerError(msg, err) everywhere; that form puts
// err.Error() directly in the HTTP response body.
func internalError(msg string, err error) huma.StatusError {
	log.Printf("%s: %v", msg, err)
	return huma.Error500InternalServerError(msg)
}

// requireAuth validates a session cookie value, returning the admin's
// email or a 401 huma error. Protected endpoint Input structs should embed
// a `SessionToken string `cookie:"itp_session"`` field and call this first.
func (s *Server) requireAuth(ctx context.Context, sessionToken string) (string, error) {
	if sessionToken == "" {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	email, err := s.Auth.ValidateSession(ctx, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return email, nil
}
