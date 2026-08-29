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
	"os"
	"os/exec"
	"strings"
)

// safeEnv returns a minimal environment for docker/docker-compose
// subprocesses — just enough for the CLI to run (PATH, HOME, and
// DOCKER_HOST/DOCKER_CONFIG if set) — deliberately NOT the backend's own
// full process environment. Without this, our own env vars (BASE_DOMAIN,
// DATABASE_URL, ADMIN_PASSWORD, ...) leak into every subprocess call; for
// `docker compose` specifically this is worse than just a leak — Compose's
// ${VAR} interpolation prioritizes an inherited shell env var over a
// project's own .env file, so an updated Settings value (or any module
// config field whose name happens to collide with one of ours) would
// silently keep resolving to whatever this container's env said at
// startup, no matter what gets freshly written to the module's .env.
func safeEnv() []string {
	var env []string
	for _, key := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONFIG"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

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
	cmd.Env = safeEnv()
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
	cmd.Env = safeEnv()
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
	cmd.Env = safeEnv()
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

// RunRcloneCopy runs `rclone copy` (never `sync`) in a throwaway
// container, mounting volume at /data — deliberately `copy`, not `sync`:
// sync mirrors deletions, so a file someone deleted from WebDAV would be
// deleted from the backup on the very next run too, defeating the most
// common reason to want a backup at all (recovering something that was
// accidentally deleted, not just full data loss). Works for both
// directions: backup mounts read-only and copies /data -> a bucket path;
// restore mounts read-write and copies a bucket path -> /data. env
// drives rclone's remote config (no config file needed) — identical
// either way for our own Garage bucket or a real AWS one. network lets
// it resolve internal container addresses like Garage's, needed for a
// local destination but harmless for a real AWS one too.
func (c *Client) RunRcloneCopy(ctx context.Context, volume string, volumeWritable bool, network string, env map[string]string, src, dst string) (string, error) {
	mode := ":ro"
	if volumeWritable {
		mode = ""
	}
	args := []string{"run", "--rm", "-v", volume + ":/data" + mode, "--network", network}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, "rclone/rclone:latest", "copy", src, dst, "-v")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = safeEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("rclone copy: %w: %s", err, string(out))
	}
	return string(out), nil
}

// Exec runs a command inside a running container and returns its combined
// output — for one-off commands like the internal gateway's `netbird up`,
// where there's no other API to drive it.
func (c *Client) Exec(ctx context.Context, containerName string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", containerName}, args...)...)
	cmd.Env = safeEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker exec %s %v: %w: %s", containerName, args, err, string(out))
	}
	return string(out), nil
}

func (c *Client) run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = safeEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %v: %w: %s", args, err, stderr.String())
	}
	return nil
}
