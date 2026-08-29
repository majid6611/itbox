package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/auth"
	"it-platform/backend/internal/docker"
	"it-platform/backend/internal/modules"
)

type Server struct {
	Auth     *auth.Service
	Docker   *docker.Client
	Modules  *modules.Manager
	Registry *modules.Registry
	DB       *pgxpool.Pool

	// GatewayContainerName is the internal VPN gateway's fixed container
	// name (see docker-compose.yaml's internal-gateway service) — needed
	// to `docker exec` its one-time NetBird enrollment.
	GatewayContainerName string
}

const sessionCookieName = "itp_session"

func NewRouter(s *Server) http.Handler {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("IT Platform API", "0.1.0"))

	registerHealth(api, s)
	registerAuth(api, s)
	registerModules(api, s)
	registerUsers(api, s)
	registerGroups(api, s)
	registerVpn(api, s)
	registerSettings(api, s)

	return router
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
