package modules

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Health reports whether every container of a module is doing what it
// should right now: services up and not failing their healthcheck, setup
// steps (restart: "no") allowed to have exited cleanly. detail names the
// offenders when it's not healthy.
func (m *Manager) Health(ctx context.Context, moduleID string) (ok bool, detail string, err error) {
	containers, err := m.docker.ProjectContainers(ctx, m.projectName(moduleID))
	if err != nil {
		return false, "", err
	}
	if len(containers) == 0 {
		return false, "no containers", nil
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
		return false, strings.Join(problems, "; "), nil
	}
	return true, "", nil
}

// WaitHealthy waits until the module has been healthy on several checks in
// a row (a crash-looping container looks "running" between restarts, so one
// good reading proves nothing), or the timeout passes. Returns why not.
func (m *Manager) WaitHealthy(ctx context.Context, moduleID string, timeout time.Duration) error {
	const needed = 4
	const every = 4 * time.Second
	deadline := time.Now().Add(timeout)
	streak := 0
	last := "not checked yet"
	for time.Now().Before(deadline) {
		ok, detail, err := m.Health(ctx, moduleID)
		switch {
		case err != nil:
			last, streak = err.Error(), 0
		case ok:
			streak++
			if streak >= needed {
				return nil
			}
		default:
			last, streak = detail, 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
	return fmt.Errorf("%s", last)
}
