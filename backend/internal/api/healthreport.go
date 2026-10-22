package api

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// HealthCheck is one line of the "is everything OK?" report.
type HealthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, bad
	Detail string `json:"detail"`
}

type HealthReportInput struct {
	SessionToken string `cookie:"itp_session"`
}

type HealthReportOutput struct {
	Body struct {
		Checks []HealthCheck `json:"checks"`
	}
}

// moduleHealth judges a module by what Docker says its containers are doing
// right now, not by the status we recorded at install — a crashed
// container still reads "running" in our own table.
func (s *Server) moduleHealth(ctx context.Context, id, name string) HealthCheck {
	check := HealthCheck{Name: name, Status: "ok", Detail: "Running"}
	containers, err := s.Docker.ProjectContainers(ctx, s.Modules.ProjectName(id))
	if err != nil {
		return HealthCheck{Name: name, Status: "warn", Detail: "Couldn't check: " + err.Error()}
	}
	if len(containers) == 0 {
		return HealthCheck{Name: name, Status: "bad", Detail: "Not running"}
	}
	var problems []string
	for _, c := range containers {
		switch {
		case c.State == "running" && c.Health != "unhealthy":
		case c.State == "exited" && c.ExitCode == 0 && c.OneShot:
			// A one-shot setup step that finished, not a failure.
		default:
			what := c.State
			if c.State == "running" {
				what = "unhealthy"
			} else if c.State == "exited" {
				what = fmt.Sprintf("stopped (exit %d)", c.ExitCode)
			}
			problems = append(problems, c.Name+": "+what)
		}
	}
	if len(problems) > 0 {
		check.Status, check.Detail = "bad", strings.Join(problems, "; ")
	}
	return check
}

func (s *Server) diskHealth() HealthCheck {
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.Modules.DataDir(), &st); err != nil {
		return HealthCheck{Name: "Disk space", Status: "warn", Detail: "Couldn't check: " + err.Error()}
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	if total == 0 {
		return HealthCheck{Name: "Disk space", Status: "warn", Detail: "Couldn't check"}
	}
	usedPct := (1 - free/total) * 100
	check := HealthCheck{Name: "Disk space", Status: "ok", Detail: fmt.Sprintf("%.0f%% used, %.1f GB free", usedPct, free/1e9)}
	switch {
	case usedPct >= 90:
		check.Status = "bad"
	case usedPct >= 80:
		check.Status = "warn"
	}
	return check
}

func (s *Server) backupHealth(ctx context.Context) HealthCheck {
	cfg, _ := s.getBackupConfig(ctx)
	var status string
	var started time.Time
	err := s.DB.QueryRow(ctx, `SELECT status, started_at FROM backup_runs WHERE kind = 'backup' ORDER BY started_at DESC LIMIT 1`).Scan(&status, &started)
	if err != nil {
		return HealthCheck{Name: "Backup", Status: "warn", Detail: "No backup has ever run"}
	}
	age := time.Since(started)
	ago := fmt.Sprintf("%.0f hours ago", age.Hours())
	if age > 48*time.Hour {
		ago = fmt.Sprintf("%.0f days ago", age.Hours()/24)
	}
	switch {
	case status == "error":
		return HealthCheck{Name: "Backup", Status: "bad", Detail: "The last backup failed (" + ago + ")"}
	case cfg.Schedule == "off":
		return HealthCheck{Name: "Backup", Status: "warn", Detail: "Last backup " + ago + "; automatic backups are off"}
	case age > 2*24*time.Hour && cfg.Schedule == "daily", age > 2*7*24*time.Hour:
		return HealthCheck{Name: "Backup", Status: "warn", Detail: "Last backup " + ago + " — overdue"}
	}
	return HealthCheck{Name: "Backup", Status: "ok", Detail: "Last backup " + ago}
}

func registerHealthReport(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "health-report",
		Method:      "GET",
		Path:        "/api/health/report",
		Summary:     "Is everything OK? Installed modules' real container state, disk space and backup freshness",
	}, func(ctx context.Context, in *HealthReportInput) (*HealthReportOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &HealthReportOutput{}
		statuses, err := s.Modules.ListStatuses(ctx)
		if err != nil {
			return nil, internalError("list modules", err)
		}
		for _, st := range statuses {
			if st.Status == "not_installed" || st.Status == "stopped" {
				continue // not expected to be running
			}
			name := st.ModuleID
			if m, ok := s.Registry.Get(st.ModuleID); ok {
				name = m.Name
			}
			if st.Status == "installing" {
				out.Body.Checks = append(out.Body.Checks, HealthCheck{Name: name, Status: "warn", Detail: "Still installing"})
				continue
			}
			if st.Status == "error" {
				out.Body.Checks = append(out.Body.Checks, HealthCheck{Name: name, Status: "bad", Detail: "Install or start failed"})
				continue
			}
			out.Body.Checks = append(out.Body.Checks, s.moduleHealth(ctx, st.ModuleID, name))
		}
		out.Body.Checks = append(out.Body.Checks, s.diskHealth(), s.backupHealth(ctx))
		return out, nil
	})
}
