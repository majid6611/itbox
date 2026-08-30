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

// internalGatewayIP is nginx's own fixed address on the edge network (see
// docker-compose.yaml's edge network IPAM config) — the one address the
// internal gateway advertises as a NetBird route, so a private route's
// address needs to be reachable there specifically, not wherever Docker
// happens to assign nginx next time it's recreated.
const internalGatewayIP = "10.201.28.2"

// RouteTarget is one hostname->upstream mapping to write for a module.
// Name is "" for a module's primary route.
//
// PrivatePort is 0 for a normal public route (reachable at Hostname on the
// shared public port). A non-zero value instead makes this route private:
// reachable only at internalGatewayIP:PrivatePort, which the public
// internet was never given a path to and only VPN-connected devices can
// reach (via the internal gateway's advertised route) — see
// modules/README or the Settings/module-visibility docs for the full
// picture. Because there's no DNS for VPN clients to tell modules apart
// by hostname the way the public path does, a private route gets its own
// dedicated port instead of sharing one via Host-header routing.
type RouteTarget struct {
	Name        string
	Hostname    string
	Upstream    string
	PrivatePort int
}

func (m *Manager) confPath(moduleID, routeName string) string {
	if routeName == "" {
		routeName = "default"
	}
	return filepath.Join(m.confDir, moduleID+"__"+routeName+".conf")
}

// pathsDir holds location-only snippets included from inside the main
// site's own server block (see nginx/templates/default.conf.template's
// `include .../paths/*.conf`) — a `location` block isn't valid outside a
// `server` block, so these can't just be dropped in confDir itself
// alongside the other modules' whole standalone server blocks.
func (m *Manager) pathsDir() string {
	return filepath.Join(m.confDir, "paths")
}

func (m *Manager) pathRouteConfPath(moduleID string) string {
	return filepath.Join(m.pathsDir(), moduleID+".conf")
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
		var conf string
		if r.PrivatePort != 0 {
			// Bound to nginx's own fixed edge-network IP, not 0.0.0.0 — this
			// listen address is never published to the host, so it's simply
			// absent from the public internet, not just access-controlled.
			// No server_name/Host-header matching: the port itself is what
			// picks the module, since a VPN client has no DNS to resolve a
			// friendly hostname to this address in the first place.
			conf = fmt.Sprintf(`server {
    listen %s:%d;

    resolver 127.0.0.11 valid=10s;

    location / {
        set $upstream "http://%s";
        proxy_pass $upstream;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
`, internalGatewayIP, r.PrivatePort, r.Upstream)
		} else {
			// The upstream is resolved lazily via a variable + Docker's embedded
			// DNS (127.0.0.11), not baked into a literal proxy_pass hostname. A
			// literal hostname is resolved once at config load, and nginx refuses
			// to start at all if it doesn't resolve — so a single stale vhost
			// (module container removed outside the API) would take down routing
			// for every module, including the platform UI. The lazy form just
			// 502s that one route instead.
			conf = fmt.Sprintf(`server {
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
		}

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

// PathRouteTarget is one URL-path-prefix -> upstream mapping for a
// feature-module served under the main site's own domain (see PathRoute
// in modules/manifest.go).
type PathRouteTarget struct {
	Path     string
	Upstream string
}

// SetPathRoutes writes (or overwrites) one module's path-based proxy
// snippet — a location block per path prefix, included from inside the
// main site's server block — then reloads nginx.
func (m *Manager) SetPathRoutes(ctx context.Context, moduleID string, targets []PathRouteTarget) error {
	if err := os.MkdirAll(m.pathsDir(), 0o755); err != nil {
		return fmt.Errorf("create nginx paths dir: %w", err)
	}
	var conf string
	for _, t := range targets {
		conf += fmt.Sprintf(`    location %s/ {
        resolver 127.0.0.11 valid=10s;
        set $upstream "http://%s";
        proxy_pass $upstream;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
`, t.Path, t.Upstream)
	}
	if err := os.WriteFile(m.pathRouteConfPath(moduleID), []byte(conf), 0o644); err != nil {
		return fmt.Errorf("write nginx path routes for %s: %w", moduleID, err)
	}
	if err := m.docker.ReloadNginx(ctx, m.containerName); err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}
	return nil
}

// RemovePathRoutes deletes a module's path-route snippet, if any, and
// reloads nginx. Safe to call for modules that never had one.
func (m *Manager) RemovePathRoutes(ctx context.Context, moduleID string) error {
	f := m.pathRouteConfPath(moduleID)
	if err := os.Remove(f); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove nginx path routes %s: %w", f, err)
	}
	if err := m.docker.ReloadNginx(ctx, m.containerName); err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}
	return nil
}
