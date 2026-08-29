// Package docker orchestrates module containers by shelling out to the
// `docker compose` CLI against the host's Docker socket, rather than
// reimplementing compose-file semantics against the low-level Engine API.
// The backend container must have the docker CLI installed and
// /var/run/docker.sock bind-mounted for this to work (docker-outside-of-docker).
package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// Ping verifies the docker CLI can reach the daemon.
func (c *Client) Ping(ctx context.Context) error {
	return c.run(ctx, "", "info", "--format", "{{.ServerVersion}}")
}

// ComposeUp brings a module's stack up in detached mode.
func (c *Client) ComposeUp(ctx context.Context, dir, project string) error {
	return c.run(ctx, dir, "compose", "-p", project, "up", "-d")
}

// ComposeDown stops and removes a module's stack's containers and network.
// Named volumes are preserved (module data is not deleted on disable/uninstall).
func (c *Client) ComposeDown(ctx context.Context, dir, project string) error {
	return c.run(ctx, dir, "compose", "-p", project, "down")
}

// ComposeStatus returns "running", "stopped", or "unknown" for a module's stack.
func (c *Client) ComposeStatus(ctx context.Context, dir, project string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", project, "ps", "--status", "running", "-q")
	cmd.Dir = dir
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown", fmt.Errorf("compose ps: %w", err)
	}
	if out.Len() > 0 {
		return "running", nil
	}
	return "stopped", nil
}

// ReloadNginx tells the edge nginx container to reload its config
// (picking up vhosts the proxy manager just wrote/removed) without
// dropping existing connections.
func (c *Client) ReloadNginx(ctx context.Context, containerName string) error {
	return c.run(ctx, "", "exec", containerName, "nginx", "-s", "reload")
}

// ReadVolumeFile reads a file out of a named Docker volume via a
// throwaway busybox container — for reading a module's own config file
// when we need to modify it out-of-band (e.g. syncing an LDAP user's
// login into WebDAV's config, which has no live API to do that).
func (c *Client) ReadVolumeFile(ctx context.Context, volume, path string) (string, error) {
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", volume+":/target:ro", "busybox", "cat", "/target/"+path)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("read %s from volume %s: %w: %s", path, volume, err, stderr.String())
	}
	return out.String(), nil
}

// WriteVolumeFile overwrites a file in a named Docker volume via a
// throwaway busybox container.
func (c *Client) WriteVolumeFile(ctx context.Context, volume, path, content string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "-v", volume+":/target", "busybox", "sh", "-c", "cat > /target/"+path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write %s to volume %s: %w: %s", path, volume, err, stderr.String())
	}
	return nil
}

// RestartContainer restarts a running container by name — e.g. to pick up
// a config file that was just rewritten out-of-band.
func (c *Client) RestartContainer(ctx context.Context, containerName string) error {
	return c.run(ctx, "", "restart", containerName)
}

func (c *Client) run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %v: %w: %s", args, err, stderr.String())
	}
	return nil
}
