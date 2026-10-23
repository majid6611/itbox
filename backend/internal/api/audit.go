package api

import (
	"context"
	"encoding/csv"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/audit"
)

// Logins are recorded by their own handlers (with the name that was tried,
// which a request alone doesn't reveal); everything else that changes
// something is recorded here, so no handler can forget.
var auditSkip = map[string]bool{"login": true, "portal-login": true}

// auditedReads are reads that still belong in the log (data leaving).
var auditedReads = map[string]bool{"export-audit-log": true, "export-system-log": true}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

// auditMiddleware records every request that changes something: who, what,
// which target, from where, and whether it worked. Request bodies are never
// stored — they carry passwords and keys.
func (s *Server) auditMiddleware(ctx huma.Context, next func(huma.Context)) {
	op := ctx.Operation()
	method := ctx.Method()
	read := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	if op == nil || auditSkip[op.OperationID] || (read && !auditedReads[op.OperationID]) {
		next(ctx)
		return
	}
	actor, kind := s.sessionActor(ctx)
	var target []string
	for _, m := range pathParam.FindAllStringSubmatch(op.Path, -1) {
		if v := ctx.Param(m[1]); v != "" {
			target = append(target, v)
		}
	}
	next(ctx)
	s.Audit.Record(audit.Entry{
		Actor: actor, ActorKind: kind,
		Action: op.OperationID, Target: strings.Join(target, "/"),
		IP: ctx.Header("X-Real-IP"), Status: ctx.Status(),
	})
}

// sessionActor names whoever the request's session cookie belongs to, or ""
// when nobody is signed in (a rejected request still gets logged).
func (s *Server) sessionActor(ctx huma.Context) (string, string) {
	r := &http.Request{Header: http.Header{"Cookie": {ctx.Header("Cookie")}}}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if email, err := s.Auth.ValidateSession(ctx.Context(), c.Value); err == nil {
			return email, "admin"
		}
	}
	if c, err := r.Cookie(employeeSessionCookieName); err == nil {
		if name, err := s.Employee.ValidateSession(ctx.Context(), c.Value); err == nil {
			return name, "employee"
		}
	}
	return "", ""
}

type AuditListInput struct {
	SessionToken string `cookie:"itp_session"`
	Actor        string `query:"actor"`
	Action       string `query:"action"`
	From         string `query:"from" doc:"YYYY-MM-DD"`
	To           string `query:"to" doc:"YYYY-MM-DD, inclusive"`
	Limit        int    `query:"limit"`
	Offset       int    `query:"offset"`
}

type AuditListOutput struct {
	Body struct {
		Entries []audit.Entry `json:"entries"`
		Total   int           `json:"total"`
	}
}

type AuditVerifyInput struct {
	SessionToken string `cookie:"itp_session"`
}

type AuditVerifyOutput struct {
	Body audit.VerifyResult
}

func (in AuditListInput) filter() (audit.Filter, error) {
	f := audit.Filter{Actor: in.Actor, Action: in.Action, Limit: in.Limit, Offset: in.Offset}
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

func registerAudit(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-audit-log",
		Method:      "GET",
		Path:        "/api/audit",
		Summary:     "Who did what — the audit log, newest first",
	}, func(ctx context.Context, in *AuditListInput) (*AuditListOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		f, err := in.filter()
		if err != nil {
			return nil, err
		}
		entries, total, err := s.Audit.List(ctx, f)
		if err != nil {
			return nil, internalError("list audit log", err)
		}
		out := &AuditListOutput{}
		out.Body.Entries, out.Body.Total = entries, total
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "verify-audit-log",
		Method:      "GET",
		Path:        "/api/audit/verify",
		Summary:     "Check that no audit entry was changed or removed",
	}, func(ctx context.Context, in *AuditVerifyInput) (*AuditVerifyOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		res, err := s.Audit.Verify(ctx)
		if err != nil {
			return nil, internalError("verify audit log", err)
		}
		return &AuditVerifyOutput{Body: res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-audit-log",
		Method:      "GET",
		Path:        "/api/audit/export",
		Summary:     "Download the audit log (current filter) as CSV",
	}, func(ctx context.Context, in *AuditListInput) (*huma.StreamResponse, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		f, err := in.filter()
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/csv; charset=utf-8")
			hctx.SetHeader("Content-Disposition", `attachment; filename="audit-log.csv"`)
			w := csv.NewWriter(hctx.BodyWriter())
			_ = w.Write([]string{"time", "who", "kind", "action", "target", "ip", "status", "detail"})
			_ = s.Audit.Each(ctx, f, func(e audit.Entry) error {
				return w.Write([]string{
					e.At.UTC().Format(time.RFC3339), csvSafe(e.Actor), e.ActorKind, e.Action,
					csvSafe(e.Target), e.IP, strconv.Itoa(e.Status), csvSafe(e.Detail),
				})
			})
			w.Flush()
		}}, nil
	})
}

// csvSafe stops a spreadsheet treating a cell as a formula — usernames and
// targets come from people.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}
