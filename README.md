# IT Platform

Self-hosted, web-based, dockerized IT platform for small companies. Services
(VPN, LDAP, fileshare, S3, wiki, chat, video calls, office phone system,
calendar, remote power management) are installed as independent modules
from a built-in Module Store, so a company only runs what it needs.

## Installation

For a real deployment (a fresh Ubuntu/Debian server, or your own machine):

```sh
git clone https://github.com/majid6611/itbox.git
cd itbox
./install.sh
```

`install.sh` installs Docker if it isn't already there, asks for the
admin account (email + a password, entered twice and hidden) and the
domain this deployment is reached at (`localhost` is fine for trying it
out — no setup needed, `*.localhost` resolves to your own machine
automatically), then starts everything. That's the whole install — no
manual `.env` editing, no compiler, nothing else to configure first.

Safe to re-run: if `.env` already exists, it's left alone by default, so
running `./install.sh` again on an already-set-up machine just
(re)starts the platform.

Once it's up, open the domain you gave it and log in with the admin
account you just set — then use the Module Store to turn on whatever the
company actually needs (Identity/LDAP is required by almost everything
else, so that's a good first install).

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

## Running (development)

For working on the platform itself, rather than installing it — builds
from source instead of using `install.sh`'s Docker-only path:

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

**Modules that ship their own code are built from source.** Their compose
file has `build: ${MODULE_DIR}/service` (the platform fills in where the
module's files are and its version), and the image is tagged with the
module's version, e.g. `it-wiki:1.0.0`. Installing or updating a module
therefore compiles it on the host, which takes a few minutes the first
time; the previous version's image stays behind so an update can roll back.
**Bump `version` in the module's `manifest.yaml` whenever its code changes**
— an unchanged version reuses the already-built image.

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
| `vpn-netbird` | implemented — self-hosted WireGuard mesh VPN (Netbird); optionally lets VPN users reach the whole office network |
| `wiki` | implemented — feature module, employee wiki with revision history and per-page permissions |
| `chat` | implemented — feature module, live group/DM chat with file sharing and presence |
| `video-jitsi` | implemented — feature module, self-hosted video calls (Jitsi Meet), linkable from chat |
| `compute-mesh` | implemented — remote power control (Intel AMT: on/off/cycle) via a self-hosted MeshCentral engine behind our own UI |
| `calendar-radicale` | implemented — feature module, shared company calendar + a personal one per employee, via a self-hosted Radicale (CalDAV) engine; also subscribable from native calendar apps |
| `voip` | implemented — feature module, office phone system (Asterisk): extensions, a main number that rings a group, one SIP outside line, voicemail, call log — all managed from our own panel; employees get their phone setup + a directory in the portal |

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

## VPN and the office network

The platform's own always-on NetBird peer, the **internal gateway**
(`internal-gateway` in `docker-compose.yaml`), is what VPN users' traffic
is routed through. It advertises:

- **Always:** nginx's fixed edge address (`10.201.28.2/32`) — how
  "private" modules are reached over the VPN. (Private modules are also
  served at their normal hostname on the office network: the vhost only
  admits private and VPN source addresses and refuses the internet. So
  "private" means "not on the internet", not "VPN required".)
- **Optionally:** the office LAN (e.g. `192.168.1.0/24`), when an admin
  turns on "VPN users can reach the office network" on the VPN page. The
  subnet is auto-detected from the host's routing table and must be a
  private range. With it on, a remote laptop or phone reaches office
  computers, printers, file shares and the phone system as if it were
  plugged in at the office. Off by default — with it off, VPN users reach
  only other VPN devices and the platform's own private modules.

Both routes are owned by `syncGatewayRoutes` (`internal/api/gateway.go`),
which converges NetBird's routes to what they should be at VPN install,
at **every backend start**, and on each settings save — so a route left
pointing at an old address (e.g. after the edge subnet changed) is
repaired automatically rather than staying broken.

Note: NetBird's default access policy lets every VPN device reach every
other one, so the office-network setting applies to all VPN users alike.

## Phone system (voip)

Two containers: **Asterisk** (the engine — Asterisk 20 LTS from Alpine's
own package, only the ~60 modules it uses loaded, ~5 MiB RAM idle) and
our own **voip** service, which stores extensions/settings in the shared
Postgres, generates Asterisk's config files into a shared volume, and
reloads Asterisk over its manager port (AMI). Admins manage it at `/voip`;
employees see their own phone setup and a company directory at
`/portal/phone`.

What callers dial: an extension (e.g. `101`) for a colleague, `0` for the
main number (rings everyone ticked for it, then a shared mailbox), `9` +
number for an outside call (needs a SIP provider configured), `*97` for
their voicemail, `*98` for the main mailbox, `*43` for an echo test.

Things to know when deploying it:

- **Ports:** Asterisk runs on the host's network (`network_mode: host`),
  so UDP **5060** (SIP) and **20000–20999** (audio) must be free on the
  server. Host networking is deliberate — behind Docker's NAT, SIP
  advertises the container's internal address and calls connect with no
  audio.
- **It binds to the server's LAN IP**, detected at container start, not
  `0.0.0.0`. The host sits on several networks (LAN + Docker bridges) and
  a `0.0.0.0` UDP socket answers from whichever address the route picks —
  VPN phones then got replies from the Docker-bridge address, which the
  VPN gateway's NAT drops. Consequence: **give the server a fixed LAN IP**
  (a DHCP reservation on the router); if its IP changes, restart the voip
  module. `SERVER_IP` can be set explicitly on the asterisk container if
  detection picks the wrong interface.
- **Phones outside the office** work over the VPN once "VPN users can
  reach the office network" is on (see above) — they register to the same
  LAN IP as office phones.
- **Security:** extensions may only register from private and VPN address
  ranges, passwords are generated (32 hex chars), and international
  outside calls are blocked unless an admin allows them.

**Why Asterisk, not FreePBX:** FreePBX is a heavy PHP admin UI layered on
Asterisk — exactly the kind of third-party admin UI this project replaces
with its own. Plain Asterisk with generated config files is lighter and
keeps our panel the only UI. FreeSWITCH was heavier and more complex for
5–50 users; Kamailio/OpenSIPS are SIP proxies, not PBXs (no voicemail or
call logic).

### Phone menu ("press 1 for IT")

One level, no sub-menus. Outside callers hear a recorded greeting and press
a key; each key (1–9) rings one or more extensions together, and falls back
to that person's voicemail (or the shared one for a group). Pressing `0`, a
key that isn't on the menu twice, or nothing, rings the main number. Staff
dialing `0` skip the menu; dialing `*50` plays it, so an admin can hear what
callers hear.

The greeting is either uploaded in the panel (any mp3/m4a/wav/ogg/flac; the
voip service converts it with ffmpeg to the 8 kHz mono WAV Asterisk plays) or
recorded by phone: the admin picks one extension and dials `*99` from it (only
that extension may — Asterisk checks which account the phone logged in as).
It lives on the `asterisk_sounds` volume, which is part of the platform
backup. Menu keys and settings are stored in `voip_ivr_options` and
`voip_settings`; `pbx.Render` turns them into the `[itp-ivr]` dialplan.

## Audit log

**Logs** in the admin panel shows who did what. Every request that changes
something (install a module, create a user, change a setting, ...) is recorded
automatically by one middleware (`backend/internal/api/audit.go`) — a new
endpoint is covered without anyone remembering to add a call. Sign-ins, failed
sign-ins and lockouts (admin and employee portal) are recorded by their own
handlers, with the name that was tried. Request bodies are never stored: they
carry passwords and keys.

- **Append-only.** The database refuses edits, and refuses deletes of anything
  younger than one year (`0017_audit_log.sql`). Entries older than a year are
  pruned daily.
- **Tamper-evident.** Each entry holds the hash of the one before it; "Check
  the log is intact" re-walks the chain and names the first entry that was
  changed or removed. This shows tampering; it can't prevent it by someone with
  full control of the server.
- **Restore never rewinds it.** `audit_log` is in `backupKeepTables`, so
  "Restore everything" leaves it alone (and it isn't part of the backup dump).
- Downloads (CSV) are recorded too.

Tests that need a real Postgres skip unless `TEST_DATABASE_URL` is set.

## Firewall

`install.sh` offers to install a host firewall (`scripts/firewall.nft`,
loaded at boot by `scripts/install-firewall.sh`). One rule: the office
network and the VPN are trusted; the internet can only reach the ports
remote users need — SSH, the web panel (80/443/8000), the VPN (8090,
3478/udp) and video calls (10000/udp). Everything else, notably the phone
system's SIP and audio ports, answers to inside addresses only.

It needs two hooks because Docker bypasses a normal host firewall:
`input` guards services on the host's own network (Asterisk), `forward`
guards published container ports, judged by the port the client dialed
rather than the container's internal one. To open or close a port, edit
the two `public_*` sets in `firewall.nft` and re-run
`scripts/install-firewall.sh`. Remove it with
`sudo systemctl disable --now itbox-firewall`.

Caveat: an outside SIP trunk provider that dials *in* without our server
registering to it first (IP-authenticated) is blocked too — add its address
to `internal4`, or open 5060/udp and the RTP range, if a customer needs
that.

## Upgrades and data safety

A version change must never delete customer data. Four rules enforce it:

- **Snapshot first.** Before applying any pending database migration (and
  before a module update that migrates the shared database, and before
  "Restore everything" replaces it), the platform saves a full `pg_dump` to
  `data/snapshots/` and refuses to continue if that fails. The last 5 are
  kept. To go back: `pg_restore --clean --if-exists --single-transaction`
  that file into the `postgres` container.
- **All or nothing.** Every migration runs in its own transaction.
- **No downgrades.** If the database was upgraded by a newer version, older
  code refuses to start instead of running against a schema it doesn't
  know. Restore a snapshot or install the newer version.
- **No destructive migrations by accident.** `go test ./internal/db/` fails
  on `DROP TABLE/COLUMN`, `TRUNCATE`, `DELETE FROM` or a column type change
  in any core or module migration, unless the file carries a
  `-- allow-destructive: <why no data is lost>` line. Prefer adding columns
  and tables; keep the old ones until a later version has stopped using them.

**Module updates** add three more steps. Before replacing anything the
platform keeps a copy of the module's current files and of its data volumes
(`data/snapshots/module-files`, `data/snapshots/volumes`), alongside the
database snapshot. After restarting on the new version it waits for every
container to stay healthy; if they don't, the old files are put back and
the old version is started again, and the Module Store shows why. The
database is **not** rolled back automatically — that would erase what users
saved meanwhile — which is safe because migrations only add, so the old
version runs on the newer schema. Restoring a snapshot is a manual choice.

Never bump the pinned `postgres:16` image casually: a major Postgres
version can't open the previous version's data directory.

## Repo layout

```
backend/    Go control-plane API
frontend/   Vue 3 admin UI
modules/    module catalog (manifest + compose fragment per module)
data/       per-install rendered compose files (gitignored)
nginx/      edge nginx: static template (platform UI) + conf.d (module vhosts, gitignored)
```
