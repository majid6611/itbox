// Package proxy manages the edge nginx's per-module vhost files. Routing a
// module means writing a small server block into the shared conf.d
// directory (bind-mounted into both this container and the nginx
// container) and telling nginx to reload; unrouting means deleting those
// files and reloading again. This avoids nginx needing to know about
// modules on its own — the control plane owns the mapping.
package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type nginxReloader interface {
	ReloadNginx(ctx context.Context, containerName string) error
}

type Manager struct {
	confDir       string
	containerName string
	docker        nginxReloader
}

func NewManager(dockerClient nginxReloader, confDir, containerName string) *Manager {
	return &Manager{docker: dockerClient, confDir: confDir, containerName: containerName}
}

// RouteTarget is one hostname->upstream mapping to write for a module.
// Name is "" for a module's primary route.
type RouteTarget struct {
	Name     string
	Hostname string
	Upstream string
}

func (m *Manager) confPath(moduleID, routeName string) string {
	if routeName == "" {
		routeName = "default"
	}
	return filepath.Join(m.confDir, moduleID+"__"+routeName+".conf")
}

// SetRoutes writes (or overwrites) a module's vhosts, one per RouteTarget,
// then reloads nginx once.
//
// A module route always gets a plain HTTP/1.1 vhost here — that's not a
// fit for every protocol (NetBird's netbird-server route is deliberately
// left out of routes entirely because its client protocol is native gRPC,
// and plaintext HTTP/2 protocol detection in nginx is a per-listen-socket
// setting, not per-vhost, so it can't share this shared port-80 socket
// with every other module's plain vhost; it gets a direct host port
// instead — see modules/vpn-netbird/docker-compose.yaml).
func (m *Manager) SetRoutes(ctx context.Context, moduleID string, routes []RouteTarget) error {
	for _, r := range routes {
		// The upstream is resolved lazily via a variable + Docker's embedded
		// DNS (127.0.0.11), not baked into a literal proxy_pass hostname. A
		// literal hostname is resolved once at config load, and nginx refuses
		// to start at all if it doesn't resolve — so a single stale vhost
		// (module container removed outside the API) would take down routing
		// for every module, including the platform UI. The lazy form just
		// 502s that one route instead.
		conf := fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    resolver 127.0.0.11 valid=10s;

    location / {
        set $upstream "http://%s";
        proxy_pass $upstream;
        # $http_host (not $host) — WebDAV MOVE/COPY carry an absolute
        # Destination header built by the client from the URL it connected
        # to, port included. Go's webdav library 502s if the forwarded
        # Host header doesn't match that exactly, and $host silently
        # strips the port, breaking rename/move for every WebDAV module.
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
`, r.Hostname, r.Upstream)

		if err := os.WriteFile(m.confPath(moduleID, r.Name), []byte(conf), 0o644); err != nil {
			return fmt.Errorf("write nginx vhost for %s/%s: %w", moduleID, r.Name, err)
		}
	}
	if err := m.docker.ReloadNginx(ctx, m.containerName); err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}
	return nil
}

// RemoveRoutes deletes all of a module's vhosts, if any, and reloads
// nginx. Safe to call for modules that were never routed.
func (m *Manager) RemoveRoutes(ctx context.Context, moduleID string) error {
	matches, err := filepath.Glob(filepath.Join(m.confDir, moduleID+"__*.conf"))
	if err != nil {
		return fmt.Errorf("list nginx vhosts for %s: %w", moduleID, err)
	}
	if len(matches) == 0 {
		return nil
	}
	for _, f := range matches {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove nginx vhost %s: %w", f, err)
		}
	}
	if err := m.docker.ReloadNginx(ctx, m.containerName); err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}
	return nil
}
