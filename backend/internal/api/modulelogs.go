package api

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type ModuleLogsInput struct {
	SessionToken string `cookie:"itp_session"`
	ID           string `path:"id" doc:"a module id, or \"platform\" for the backend itself"`
	Lines        int    `query:"lines"`
	Download     bool   `query:"download"`
}

// Patterns for log text that looks like a credential. Output is meant to be
// shown and sent to people, so these values are masked.
var (
	bearerValue = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`)
	secretPair  = regexp.MustCompile(`(?i)((?:pass(?:word|wd)?|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|setup[_-]?key|authorization)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;"'&]+)`)
	secretFlag  = regexp.MustCompile(`(?i)(--[a-z-]*(?:pass|secret|token|key)[a-z-]*\s+)\S+`)
)

func redact(s string) string {
	s = bearerValue.ReplaceAllString(s, "$1 [hidden]")
	s = secretPair.ReplaceAllString(s, "${1}[hidden]")
	return secretFlag.ReplaceAllString(s, "${1}[hidden]")
}

func registerModuleLogs(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "module-logs",
		Method:      "GET",
		Path:        "/api/modules/{id}/logs",
		Summary:     "The last lines a module's containers wrote (passwords and keys masked)",
	}, func(ctx context.Context, in *ModuleLogsInput) (*huma.StreamResponse, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		lines := in.Lines
		if lines <= 0 {
			lines = 300
		}
		if lines > 5000 {
			lines = 5000
		}
		var text string
		var err error
		if in.ID == "platform" {
			host, _ := os.Hostname()
			text, err = s.Docker.SelfLogs(ctx, host, lines)
		} else {
			// Only a module that exists here: never an arbitrary project name.
			if _, ok := s.Registry.Get(in.ID); !ok {
				return nil, huma.Error404NotFound("no such module")
			}
			text, err = s.Docker.ProjectLogs(ctx, s.Modules.ProjectName(in.ID), lines)
		}
		if err != nil {
			return nil, internalError("read module logs", err)
		}
		text = redact(text)
		if strings.TrimSpace(text) == "" {
			text = "(nothing logged yet)\n"
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
			if in.Download {
				hctx.SetHeader("Content-Disposition", `attachment; filename="`+in.ID+`-log.txt"`)
			}
			_, _ = hctx.BodyWriter().Write([]byte(text))
		}}, nil
	})
}
