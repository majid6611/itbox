package docker

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ProjectLogs returns the last `tail` lines of every container in a compose
// project, merged oldest-first, each line tagged with its container.
func (c *Client) ProjectLogs(ctx context.Context, project string, tail int) (string, error) {
	ids, err := c.output(ctx, "ps", "-aq", "--filter", "label=com.docker.compose.project="+project)
	if err != nil {
		return "", err
	}
	return c.logsOf(ctx, strings.Fields(ids), tail)
}

// SelfLogs returns the last lines of the container this process runs in
// (inside a container the hostname is the container id) — the platform's
// own backend output.
func (c *Client) SelfLogs(ctx context.Context, hostname string, tail int) (string, error) {
	return c.logsOf(ctx, []string{hostname}, tail)
}

func (c *Client) logsOf(ctx context.Context, containers []string, tail int) (string, error) {
	type line struct{ ts, text string }
	var lines []line
	for _, id := range containers {
		// stdout and stderr both: that is where programs write errors.
		cmd := exec.CommandContext(ctx, "docker", "logs", "--timestamps", "--tail", fmt.Sprint(tail), id)
		cmd.Env = safeEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker logs: %w", err)
		}
		name := id
		if n, err := c.output(ctx, "inspect", "--format", "{{.Name}}", id); err == nil {
			name = strings.TrimPrefix(strings.TrimSpace(n), "/")
		}
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if l == "" {
				continue
			}
			ts, text, _ := strings.Cut(l, " ")
			lines = append(lines, line{ts, "[" + name + "] " + text})
		}
	}
	// Docker's timestamps are all UTC RFC3339, so they sort as text.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].ts < lines[j].ts })
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.ts + " " + l.text + "\n")
	}
	return b.String(), nil
}
