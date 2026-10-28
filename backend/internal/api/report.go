package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/version"
)

const (
	reportEvery   = 5 * time.Minute
	reportMaxLogs = 200
	reportLogAge  = 7 * 24 * time.Hour // never dig further back than this on a first send
)

// licenceValid is the one place that decides whether a licence key is good.
// Every key is accepted for now; real checking plugs in here.
func licenceValid(key string) bool {
	return key != ""
}

type licenceRow struct {
	InstallID      string
	Key            string
	SendMonitoring bool
	LastLogID      int64
	LastSentAt     *time.Time
	LastError      string
}

func (s *Server) getLicence(ctx context.Context) (licenceRow, error) {
	var l licenceRow
	err := s.DB.QueryRow(ctx, `SELECT install_id, licence_key, send_monitoring, last_log_id, last_sent_at, last_error FROM licence`).
		Scan(&l.InstallID, &l.Key, &l.SendMonitoring, &l.LastLogID, &l.LastSentAt, &l.LastError)
	return l, err
}

// What is sent: how the machine is doing, the health list, and warnings and
// errors from the system log. Nothing about people or their files.
type reportMachine struct {
	CPU        float64 `json:"cpu_pct"`
	Mem        float64 `json:"mem_pct"`
	Disk       float64 `json:"disk_pct"`
	DiskFreeGB float64 `json:"disk_free_gb"`
	Load1      float64 `json:"load1"`
	UptimeSecs int64   `json:"uptime_secs"`
}

type reportModule struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

type reportLog struct {
	ID      int64  `json:"id"`
	At      string `json:"at"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

type Report struct {
	InstallID       string         `json:"install_id"`
	PlatformVersion string         `json:"platform_version"`
	SentAt          string         `json:"sent_at"`
	Machine         reportMachine  `json:"machine"`
	Checks          []HealthCheck  `json:"checks"`
	Modules         []reportModule `json:"modules"`
	Logs            []reportLog    `json:"logs"`
}

var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// scrub keeps email addresses (the one kind of personal detail a log line
// can carry) out of what leaves the building.
func scrub(msg string) string { return emailRe.ReplaceAllString(msg, "[email]") }

// cpuPct is busy time since the previous call, from /proc/stat. The first
// call has nothing to compare with and reports 0.
var cpuPrev struct {
	sync.Mutex
	busy, total float64
}

func cpuPct() float64 {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 6 || f[0] != "cpu" {
		return 0
	}
	var total, idle float64
	for i, v := range f[1:] {
		n, _ := strconv.ParseFloat(v, 64)
		if i >= 8 {
			break // guest time is already inside user time
		}
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	busy := total - idle
	cpuPrev.Lock()
	defer cpuPrev.Unlock()
	pb, pt := cpuPrev.busy, cpuPrev.total
	cpuPrev.busy, cpuPrev.total = busy, total
	if pt == 0 || total <= pt {
		return 0
	}
	return (busy - pb) / (total - pt) * 100
}

func uptimeSecs() int64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(raw))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return int64(v)
}

// buildReport collects what would be sent right now. Logs are the warnings
// and errors after the last one already reported.
func (s *Server) buildReport(ctx context.Context, l licenceRow) (*Report, error) {
	r := &Report{InstallID: l.InstallID, PlatformVersion: version.Version, SentAt: time.Now().UTC().Format(time.RFC3339), Checks: []HealthCheck{}, Modules: []reportModule{}, Logs: []reportLog{}}
	r.Machine.CPU = cpuPct()
	r.Machine.Mem, _ = memUsedPct()
	r.Machine.Disk, _ = diskUsedPct(s.Modules.DataDir())
	r.Machine.Load1, _ = load1()
	r.Machine.UptimeSecs = uptimeSecs()
	if free, err := diskFreeGB(s.Modules.DataDir()); err == nil {
		r.Machine.DiskFreeGB = free
	}
	if checks, err := s.healthChecks(ctx); err == nil {
		r.Checks = checks
	}
	if statuses, err := s.Modules.ListStatuses(ctx); err == nil {
		for _, st := range statuses {
			if st.Status == "not_installed" {
				continue
			}
			m := reportModule{ID: st.ModuleID, Status: st.Status}
			if man, ok := s.Registry.Get(st.ModuleID); ok {
				m.Version = man.Version
			}
			r.Modules = append(r.Modules, m)
		}
	}
	rows, err := s.DB.Query(ctx, `SELECT id, to_char(at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), level, source, message FROM system_log WHERE id > $1 AND at > $2 AND level IN ('warn','error') ORDER BY id LIMIT $3`,
		l.LastLogID, time.Now().Add(-reportLogAge), reportMaxLogs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x reportLog
		if err := rows.Scan(&x.ID, &x.At, &x.Level, &x.Source, &x.Message); err != nil {
			return nil, err
		}
		x.Message = scrub(x.Message)
		r.Logs = append(r.Logs, x)
	}
	return r, rows.Err()
}

// monitorEnabled: sending needs a vendor address, a valid licence and the
// customer's own switch left on.
func (s *Server) monitorEnabled(l licenceRow) bool {
	return s.MonitorURL != "" && s.MonitorURL != "off" && licenceValid(l.Key) && l.SendMonitoring
}

var reportClient = &http.Client{Timeout: 20 * time.Second}

// sendReport sends one report and records the outcome. The log cursor only
// moves after the vendor accepted it, so a failed send is retried with the
// same lines next time.
func (s *Server) sendReport(ctx context.Context) error {
	l, err := s.getLicence(ctx)
	if err != nil {
		return err
	}
	if !s.monitorEnabled(l) {
		return fmt.Errorf("monitoring reports are not enabled")
	}
	r, err := s.buildReport(ctx, l)
	if err != nil {
		return err
	}
	err = s.post(ctx, l, r)
	if err != nil {
		_, _ = s.DB.Exec(ctx, `UPDATE licence SET last_error = $1`, clipText(err.Error(), 300))
		return err
	}
	last := l.LastLogID
	if n := len(r.Logs); n > 0 {
		last = r.Logs[n-1].ID
	}
	_, _ = s.DB.Exec(ctx, `UPDATE licence SET last_log_id = $1, last_sent_at = now(), last_error = ''`, last)
	return nil
}

func (s *Server) post(ctx context.Context, l licenceRow, r *Report) error {
	body, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(s.MonitorURL, "/")+"/api/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.Key)
	req.Header.Set("X-Install-ID", l.InstallID)
	resp, err := reportClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the monitoring server: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return fmt.Errorf("the monitoring server rejected the licence")
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("the monitoring server answered %d", resp.StatusCode)
	}
	return nil
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *Server) startReporter(ctx context.Context) {
	if s.Modules == nil || s.DB == nil {
		return
	}
	go func() {
		select { // let the platform settle, and give CPU a first reading
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		cpuPct()
		for {
			if l, err := s.getLicence(ctx); err == nil && s.monitorEnabled(l) {
				_ = s.sendReport(ctx) // the outcome is stored in last_error
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(reportEvery):
			}
		}
	}()
}

type LicenceInput struct {
	SessionToken string `cookie:"itp_session"`
}

type LicenceBody struct {
	InstallID      string `json:"install_id"`
	LicenceKey     string `json:"licence_key"`
	Licensed       bool   `json:"licensed"`
	SendMonitoring bool   `json:"send_monitoring"`
	MonitorURL     string `json:"monitor_url"`
	Sending        bool   `json:"sending"` // reports are actually going out
	LastSentAt     string `json:"last_sent_at"`
	LastError      string `json:"last_error"`
}

type LicenceOutput struct{ Body LicenceBody }

func (s *Server) licenceBody(l licenceRow) LicenceBody {
	b := LicenceBody{InstallID: l.InstallID, LicenceKey: l.Key, Licensed: licenceValid(l.Key), SendMonitoring: l.SendMonitoring, MonitorURL: s.MonitorURL, Sending: s.monitorEnabled(l), LastError: l.LastError}
	if l.LastSentAt != nil {
		b.LastSentAt = l.LastSentAt.UTC().Format(time.RFC3339)
	}
	return b
}

type SetLicenceInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		LicenceKey     string `json:"licence_key"`
		SendMonitoring bool   `json:"send_monitoring"`
	}
}

type PreviewOutput struct{ Body Report }

type SendNowOutput struct {
	Body struct {
		Licence LicenceBody `json:"licence"`
	}
}

func registerLicence(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-licence",
		Method:      "GET",
		Path:        "/api/licence",
		Summary:     "This install's licence and monitoring-report status",
	}, func(ctx context.Context, in *LicenceInput) (*LicenceOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		l, err := s.getLicence(ctx)
		if err != nil {
			return nil, internalError("read licence", err)
		}
		return &LicenceOutput{Body: s.licenceBody(l)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-licence",
		Method:      "POST",
		Path:        "/api/licence",
		Summary:     "Save the licence key and the monitoring on/off switch",
	}, func(ctx context.Context, in *SetLicenceInput) (*LicenceOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(in.Body.LicenceKey)
		if len(key) > 200 || strings.ContainsAny(key, " \t\r\n") {
			return nil, huma.Error400BadRequest("that doesn't look like a licence key")
		}
		if _, err := s.DB.Exec(ctx, `UPDATE licence SET licence_key = $1, send_monitoring = $2, last_error = ''`, key, in.Body.SendMonitoring); err != nil {
			return nil, internalError("save licence", err)
		}
		l, err := s.getLicence(ctx)
		if err != nil {
			return nil, internalError("read licence", err)
		}
		return &LicenceOutput{Body: s.licenceBody(l)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "licence-preview",
		Method:      "GET",
		Path:        "/api/licence/preview",
		Summary:     "Exactly what the next monitoring report would contain",
	}, func(ctx context.Context, in *LicenceInput) (*PreviewOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		l, err := s.getLicence(ctx)
		if err != nil {
			return nil, internalError("read licence", err)
		}
		r, err := s.buildReport(ctx, l)
		if err != nil {
			return nil, internalError("build report", err)
		}
		return &PreviewOutput{Body: *r}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "licence-send-now",
		Method:      "POST",
		Path:        "/api/licence/send",
		Summary:     "Send a monitoring report now",
	}, func(ctx context.Context, in *LicenceInput) (*SendNowOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if err := s.sendReport(ctx); err != nil {
			return nil, huma.Error502BadGateway(err.Error())
		}
		l, err := s.getLicence(ctx)
		if err != nil {
			return nil, internalError("read licence", err)
		}
		out := &SendNowOutput{}
		out.Body.Licence = s.licenceBody(l)
		return out, nil
	})
}
