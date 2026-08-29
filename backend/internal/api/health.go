package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type HealthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
		Docker bool   `json:"docker"`
	}
}

func registerHealth(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      "GET",
		Path:        "/api/health",
		Summary:     "Health check",
	}, func(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
		out := &HealthOutput{}
		out.Body.Status = "ok"
		out.Body.Docker = s.Docker.Ping(ctx) == nil
		return out, nil
	})
}
