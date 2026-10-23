package api

import (
	"context"
	"encoding/csv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/syslog"
)

type SystemLogInput struct {
	SessionToken string `cookie:"itp_session"`
	Level        string `query:"level" doc:"warn = warnings and errors, error = errors only"`
	Source       string `query:"source"`
	Search       string `query:"search"`
	From         string `query:"from" doc:"YYYY-MM-DD"`
	To           string `query:"to" doc:"YYYY-MM-DD, inclusive"`
	Limit        int    `query:"limit"`
	Offset       int    `query:"offset"`
}

type SystemLogOutput struct {
	Body struct {
		Entries []syslog.Event `json:"entries"`
		Total   int            `json:"total"`
		Sources []string       `json:"sources"`
	}
}

func (in SystemLogInput) filter() (syslog.Filter, error) {
	f := syslog.Filter{Level: in.Level, Source: in.Source, Search: in.Search, Limit: in.Limit, Offset: in.Offset}
	var err error
	if in.From != "" {
		if f.From, err = time.Parse("2006-01-02", in.From); err != nil {
			return f, huma.Error400BadRequest("from must be a date like 2026-10-23")
		}
	}
	if in.To != "" {
		to, err := time.Parse("2006-01-02", in.To)
		if err != nil {
			return f, huma.Error400BadRequest("to must be a date like 2026-10-23")
		}
		f.To = to.AddDate(0, 0, 1)
	}
	return f, nil
}

func registerSystemLog(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-system-log",
		Method:      "GET",
		Path:        "/api/system-log",
		Summary:     "What the platform and its modules did — the system log, newest first",
	}, func(ctx context.Context, in *SystemLogInput) (*SystemLogOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		f, err := in.filter()
		if err != nil {
			return nil, err
		}
		entries, total, err := s.SystemLog.List(ctx, f)
		if err != nil {
			return nil, internalError("list system log", err)
		}
		sources, err := s.SystemLog.Sources(ctx)
		if err != nil {
			return nil, internalError("list system log sources", err)
		}
		out := &SystemLogOutput{}
		out.Body.Entries, out.Body.Total, out.Body.Sources = entries, total, sources
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-system-log",
		Method:      "GET",
		Path:        "/api/system-log/export",
		Summary:     "Download the system log (current filter) as CSV",
	}, func(ctx context.Context, in *SystemLogInput) (*huma.StreamResponse, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		f, err := in.filter()
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/csv; charset=utf-8")
			hctx.SetHeader("Content-Disposition", `attachment; filename="system-log.csv"`)
			w := csv.NewWriter(hctx.BodyWriter())
			_ = w.Write([]string{"time", "level", "source", "message"})
			_ = s.SystemLog.Each(ctx, f, func(e syslog.Event) error {
				return w.Write([]string{e.At.UTC().Format(time.RFC3339), e.Level, csvSafe(e.Source), csvSafe(e.Message)})
			})
			w.Flush()
		}}, nil
	})
}
