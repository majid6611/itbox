package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/chat/internal/auth"
	"it-platform/chat/internal/directory"
	"it-platform/chat/internal/hub"
	"it-platform/chat/internal/s3client"
)

const employeeSessionCookieName = "itp_employee_session"

type Server struct {
	DB  *pgxpool.Pool
	Hub *hub.Hub
}

func NewRouter(s *Server) http.Handler {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Chat Module API", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "chat-health",
		Method:      "GET",
		Path:        "/api/portal/chat/health",
		Summary:     "Health check",
	}, func(ctx context.Context, in *HealthInput) (*HealthOutput, error) {
		out := &HealthOutput{}
		out.Body.OK = true
		return out, nil
	})

	registerChannels(api, s)
	registerMessages(api, s)
	registerAttachments(api, s)

	// The WebSocket upgrade isn't a huma operation — it needs the raw
	// http.ResponseWriter/Request the coder/websocket library upgrades
	// directly, so it's registered on the chi router underneath instead.
	router.Get("/api/portal/chat/ws", s.handleWebSocket)

	return router
}

func (s *Server) requireEmployeeAuth(ctx context.Context, sessionToken string) (string, error) {
	username, err := auth.ValidateEmployeeSession(ctx, s.DB, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return username, nil
}

func (s *Server) chatS3Client(ctx context.Context) (*s3client.Client, bool, error) {
	return s3client.FromInstalledModules(ctx, s.DB)
}

func (s *Server) employeeGroup(ctx context.Context, username string) (string, error) {
	return directory.GroupFor(ctx, s.DB, username)
}
