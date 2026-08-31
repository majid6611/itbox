# IT Platform

Self-hosted, web-based, dockerized IT platform for small companies. Services
(VPN, LDAP, fileshare, S3, wiki, chat, video calls, remote power
management) are installed as independent modules from a built-in Module
Store, so a company only runs what it needs.

## Stack

- Backend: Go (Huma v2 + chi), talks to the Docker Engine via the `docker`
  CLI (shelled out from inside the backend container, socket-mounted).
- Frontend: Vue 3 + TypeScript.
- Database: PostgreSQL — holds only control-plane state (admin users,
  sessions, which modules are installed and their config). Modules own
  their own data entirely; the platform never duplicates it.
- Edge proxy: nginx — see "Routing" below. The backend owns routing: it
  writes/removes per-module vhost files into a shared conf.d directory and
  tells nginx to reload, so modules become reachable at their own
  hostname on install without any manual proxy config.

## Running

```sh
cp .env.example .env   # fill in real secrets, set BASE_DOMAIN
docker compose up --build
```

Frontend: http://localhost:8000 (Host header `localhost`, routed through the edge nginx)
Backend health check: http://localhost:8000/api/health

For local development with hot reload:

```sh
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml up --build
```

Dev mode talks to the Vite dev server directly at http://localhost:5173
(bypassing the edge nginx, to avoid routing HMR websockets through it);
the edge nginx still runs and will route to any installed module normally.

## Module system

Each module lives under `modules/<id>/` as:

- `manifest.yaml` — metadata, a `config_schema` that drives the dynamic
  install form in the frontend, and optional `soft_dependencies` on other
  modules (e.g. VPN offering SSO if the identity module is installed).
  A module with no `compose_file` is a catalog stub ("coming soon").
- `docker-compose.yaml` — the module's stack. `${VAR}` placeholders are
  filled from the resolved config at install time.

A `config_schema` field with `type: secret` and no `default` is
auto-generated (32 random bytes, hex-encoded) at install time instead of
being asked for — for things like internal RPC/admin tokens a non-IT user
shouldn't have to invent. Add `hidden: true` to also keep it out of the
install form entirely (the frontend never renders it); omit `hidden` for
secrets the user might reasonably want to see/set, like an S3 access key
(see `modules/s3-storage/manifest.yaml` for both cases).

On install, the backend copies the module's compose file plus a generated
`.env` into `data/modules/<id>/` and runs `docker compose up -d` against it
under project name `itp-<id>`. Disable/uninstall run `docker compose down`
(named volumes are preserved; only the rendered compose/.env is removed on
uninstall). See `internal/modules/lifecycle.go` and `internal/docker/`.

**Constraint on module compose files:** the backend runs `docker compose`
from inside its own container against the *host's* Docker socket
(docker-outside-of-docker). Relative bind-mount paths in a module's
`docker-compose.yaml` will not resolve correctly this way — **use named
Docker volumes for module data**, not host bind mounts.

## Routing

The edge nginx (`docker-compose.yaml`, container `itplatform-nginx`) is the
single entrypoint (host port 8000 → container port 80). Unlike a
label-discovery proxy, it doesn't know about modules on its own — the
**backend owns routing**:

1. A module's manifest declares zero or more `routes: [{ name, service,
   port }]` — the compose service name and container port to expose for
   each. `name` is optional: the primary route (no name, or the first one
   in a module with just one) is published at `<module-id>.${BASE_DOMAIN}`;
   any additional named route at `<name>.<module-id>.${BASE_DOMAIN}` (e.g.
   `modules/s3-storage/manifest.yaml` routes both its web console at
   `s3-storage.${BASE_DOMAIN}` and its raw S3 API at
   `api.s3-storage.${BASE_DOMAIN}`). Modules with no web UI declare none.
2. Every routed service still needs to be reachable over the network —
   attach it to the external `it-platform-edge` network (see
   `modules/s3-storage/docker-compose.yaml`).
3. On install/enable, the backend (`internal/proxy`) writes one small
   nginx server block per route into a conf.d directory shared
   (bind-mounted) with the nginx container, pointing at the module's
   container by `docker compose`'s deterministic naming
   (`itp-<module-id>-<service>-1:<port>`), then runs
   `docker exec itplatform-nginx nginx -s reload` once. Disable/uninstall
   delete all of that module's vhost files and reload again.

The `/api/modules` response includes a `links` map (module id -> `[{name,
hostname}]`) so the frontend can render clickable links for each running
module's routes without re-deriving the hostname convention itself — see
`ModuleStore.vue`.

The main platform UI is served by a static template
(`nginx/templates/default.conf.template`, using nginx's built-in
envsubst-on-start) at the bare `${BASE_DOMAIN}` — its own nginx still
proxies `/api` to the backend internally, unchanged.

Production TLS isn't wired up yet — follow-on work when a real domain is
in play.

Modules currently in the catalog:

| id | status |
|---|---|
| `s3-storage` | implemented — S3-compatible object storage (Garage + web console) |
| `fileshare-webdav` | implemented — network file share (WebDAV), mounts as a drive in Explorer/Finder |
| `ldap-openldap` | implemented — company directory (OpenLDAP), managed from our own Users panel |
| `vpn-netbird` | implemented — self-hosted WireGuard mesh VPN (Netbird) |
| `wiki` | implemented — feature module, employee wiki with revision history and per-page permissions |
| `chat` | implemented — feature module, live group/DM chat with file sharing and presence |
| `video-jitsi` | implemented — feature module, self-hosted video calls (Jitsi Meet), linkable from chat |
| `compute-mesh` | implemented — remote power control (Intel AMT: on/off/cycle) via a self-hosted MeshCentral engine behind our own UI |

**Why Garage, not MinIO:** MinIO was the original pick, but its
community edition's web console was stripped out in mid-2025 and the
whole project went source-only, then archived in April 2026 — no more
releases, patches, or official images. Garage (Deuxfleurs) is the actively
maintained replacement, paired with the third-party `garage-webui` for
the admin console.

**Why OpenLDAP, not Authentik:** Authentik was the original pick but is a
full-featured enterprise IdP (its own Postgres + worker process, a ~2GB
image, a deep admin UI) — a lot of moving parts for "create a user, put
them in a group, reset a password." OpenLDAP has *no* web UI of its own;
the backend talks to it directly over the LDAP protocol
(`internal/directory`, using `go-ldap/ldap`), and the platform's own Users
panel is the only UI — consistent with this project's rule that
third-party admin UIs get replaced with ones we build ourselves, services
get installed as-is. Image: `vegardit/openldap` — the once-dominant
`osixia/openldap` has been unmaintained since 2021 and Bitnami's is now
paywalled (same pattern as the MinIO situation above).

## Repo layout

```
backend/    Go control-plane API
frontend/   Vue 3 admin UI
modules/    module catalog (manifest + compose fragment per module)
data/       per-install rendered compose files (gitignored)
nginx/      edge nginx: static template (platform UI) + conf.d (module vhosts, gitignored)
```
