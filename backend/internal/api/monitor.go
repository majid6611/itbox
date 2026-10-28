package api

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

const (
	sampleEvery   = 5 * time.Minute
	sampleKeep    = 30 * 24 * time.Hour
	historyWindow = 7 * 24 * time.Hour
)

// memUsedPct is how much RAM is really taken: MemAvailable already counts
// reclaimable cache as free, so a busy file cache doesn't look like a leak.
func memUsedPct() (float64, error) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var total, avail float64
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, _ = strconv.ParseFloat(f[1], 64)
		case "MemAvailable:":
			avail, _ = strconv.ParseFloat(f[1], 64)
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("no MemTotal in /proc/meminfo")
	}
	return (1 - avail/total) * 100, nil
}

func load1() (float64, error) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(raw))
	if len(f) == 0 {
		return 0, fmt.Errorf("empty /proc/loadavg")
	}
	return strconv.ParseFloat(f[0], 64)
}

func diskUsedPct(dir string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 0, fmt.Errorf("no blocks")
	}
	return (1 - float64(st.Bavail)/float64(st.Blocks)) * 100, nil
}

func diskFreeGB(dir string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return float64(st.Bavail) * float64(st.Bsize) / 1e9, nil
}

func (s *Server) memoryHealth() HealthCheck {
	pct, err := memUsedPct()
	if err != nil {
		return HealthCheck{Name: "Memory", Status: "warn", Detail: "Couldn't check: " + err.Error()}
	}
	check := HealthCheck{Name: "Memory", Status: "ok", Detail: fmt.Sprintf("%.0f%% used", pct)}
	switch {
	case pct >= 95:
		check.Status = "bad"
	case pct >= 85:
		check.Status = "warn"
	}
	return check
}

// startSampler records one row every few minutes (and prunes old ones).
func (s *Server) startSampler(ctx context.Context) {
	if s.Modules == nil || s.DB == nil {
		return
	}
	go func() {
		t := time.NewTicker(sampleEvery)
		defer t.Stop()
		for {
			s.sampleOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (s *Server) sampleOnce(ctx context.Context) {
	disk, err1 := diskUsedPct(s.Modules.DataDir())
	mem, err2 := memUsedPct()
	load, err3 := load1()
	if err1 != nil || err2 != nil || err3 != nil {
		return // a half-empty point would draw a misleading line
	}
	var bad, warn int
	if checks, err := s.healthChecks(ctx); err == nil {
		for _, c := range checks {
			switch c.Status {
			case "bad":
				bad++
			case "warn":
				warn++
			}
		}
	}
	_, _ = s.DB.Exec(ctx, `INSERT INTO health_samples (ts, disk_pct, mem_pct, load1, bad, warn) VALUES (now(), $1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, disk, mem, load, bad, warn)
	_, _ = s.DB.Exec(ctx, `DELETE FROM health_samples WHERE ts < $1`, time.Now().Add(-sampleKeep))
}

type HealthSample struct {
	Time string  `json:"t"`
	Disk float64 `json:"disk"`
	Mem  float64 `json:"mem"`
	Load float64 `json:"load"`
	Bad  int     `json:"bad"`
	Warn int     `json:"warn"`
}

type HealthHistoryOutput struct {
	Body struct {
		Samples []HealthSample `json:"samples"`
	}
}

func registerHealthHistory(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "health-history",
		Method:      "GET",
		Path:        "/api/health/history",
		Summary:     "Disk, memory and load over the last 7 days (one point per 5 minutes)",
	}, func(ctx context.Context, in *HealthReportInput) (*HealthHistoryOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		rows, err := s.DB.Query(ctx, `SELECT to_char(ts AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), disk_pct, mem_pct, load1, bad, warn FROM health_samples WHERE ts > $1 ORDER BY ts`, time.Now().Add(-historyWindow))
		if err != nil {
			return nil, internalError("read health history", err)
		}
		defer rows.Close()
		out := &HealthHistoryOutput{}
		out.Body.Samples = []HealthSample{}
		for rows.Next() {
			var x HealthSample
			if err := rows.Scan(&x.Time, &x.Disk, &x.Mem, &x.Load, &x.Bad, &x.Warn); err != nil {
				return nil, internalError("read health history", err)
			}
			out.Body.Samples = append(out.Body.Samples, x)
		}
		return out, nil
	})
}
