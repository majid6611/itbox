package modules

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/backend/internal/docker"
	"it-platform/backend/internal/proxy"
)

type Status struct {
	ModuleID     string            `json:"module_id"`
	Status       string            `json:"status"` // not_installed | installing | running | stopped | error
	Config       map[string]string `json:"config"`
	InstalledAt  *string           `json:"installed_at,omitempty"`
	ErrorMessage *string           `json:"error_message,omitempty"`
	Visibility   string            `json:"visibility"` // "public" or "private"
	// PrivatePort is set only when Visibility is "private" and a port has
	// actually been allocated (i.e. the module has been routed at least
	// once since going private) — nil otherwise.
	PrivatePort *int `json:"private_port,omitempty"`
}

type Manager struct {
	registry *Registry
	docker   *docker.Client
	proxy    *proxy.Manager
	db       *pgxpool.Pool
	dataDir  string

	// baseDomain is mutable at runtime (see SetBaseDomain) — an admin can
	// change it from the Settings panel without a redeploy, so every
	// access goes through the mutex instead of treating it as read-only
	// after construction.
	baseDomainMu sync.RWMutex
	baseDomain   string

	// inFlight guards against two lifecycle operations running
	// concurrently for the same module — e.g. a double-click, or a retry
	// firing before the first attempt's response comes back. Without
	// this, two concurrent Installs can race: one's rendered secrets end
	// up in the database record while a *different* one's secrets are
	// what a service (like Postgres) actually initialized with, leaving
	// the module permanently unable to authenticate to its own database.
	inFlight sync.Map // moduleID string -> struct{}{}
}

// NewManager loads the platform's base domain from the database if a
// Settings save has ever persisted one, otherwise seeds that row from
// defaultBaseDomain (the BASE_DOMAIN env var) so existing deployments
// keep working unchanged and there's a row for a future Settings save to
// update.
func NewManager(ctx context.Context, registry *Registry, dockerClient *docker.Client, proxyManager *proxy.Manager, db *pgxpool.Pool, dataDir, defaultBaseDomain string) (*Manager, error) {
	baseDomain := defaultBaseDomain
	var stored string
	err := db.QueryRow(ctx, `SELECT base_domain FROM platform_settings WHERE id = true`).Scan(&stored)
	switch {
	case err == nil:
		baseDomain = stored
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := db.Exec(ctx, `INSERT INTO platform_settings (id, base_domain) VALUES (true, $1)`, defaultBaseDomain); err != nil {
			return nil, fmt.Errorf("seed platform_settings: %w", err)
		}
	default:
		return nil, fmt.Errorf("load base domain: %w", err)
	}
	return &Manager{registry: registry, docker: dockerClient, proxy: proxyManager, db: db, dataDir: dataDir, baseDomain: baseDomain}, nil
}

// beginOp claims exclusive access to a module for the duration of a
// lifecycle operation. Call the returned func to release it — for Install,
// that's at the end of the background goroutine, not when Install itself
// returns. Returns an error if another operation is already in progress.
func (m *Manager) beginOp(moduleID string) (func(), error) {
	if _, loaded := m.inFlight.LoadOrStore(moduleID, struct{}{}); loaded {
		return nil, fmt.Errorf("module %q already has an install/enable/disable/uninstall in progress", moduleID)
	}
	return func() { m.inFlight.Delete(moduleID) }, nil
}

func (m *Manager) projectDir(moduleID string) string {
	return filepath.Join(m.dataDir, moduleID)
}

func (m *Manager) projectName(moduleID string) string {
	return "itp-" + moduleID
}

// BaseDomain returns the domain every module's URL is currently built
// from.
func (m *Manager) BaseDomain() string {
	m.baseDomainMu.RLock()
	defer m.baseDomainMu.RUnlock()
	return m.baseDomain
}

// SetBaseDomain persists a new domain and applies it immediately in
// memory. It only affects modules installed *after* this call — an
// already-running module's domain is baked into its own generated
// config (nginx vhost, and for some modules like NetBird, its own
// config.yaml) at install time, so changing it here doesn't retroactively
// migrate anything already installed.
func (m *Manager) SetBaseDomain(ctx context.Context, domain string) error {
	_, err := m.db.Exec(ctx, `
		INSERT INTO platform_settings (id, base_domain) VALUES (true, $1)
		ON CONFLICT (id) DO UPDATE SET base_domain = $1
	`, domain)
	if err != nil {
		return fmt.Errorf("save base domain: %w", err)
	}
	m.baseDomainMu.Lock()
	m.baseDomain = domain
	m.baseDomainMu.Unlock()
	return nil
}

// Hostname is where a route is published: the bare "<module>.<domain>"
// for the primary (unnamed) route, "<name>.<module>.<domain>" otherwise.
func (m *Manager) Hostname(moduleID, routeName string) string {
	domain := m.BaseDomain()
	if routeName == "" {
		return fmt.Sprintf("%s.%s", moduleID, domain)
	}
	return fmt.Sprintf("%s.%s.%s", routeName, moduleID, domain)
}

// upstream is the container:port a module's route target resolves to,
// following `docker compose`'s deterministic container naming
// (<project>-<service>-<index>). Assumes a single instance of the service.
func (m *Manager) upstream(moduleID string, route Route) string {
	return fmt.Sprintf("%s-%s-1:%d", m.projectName(moduleID), route.Service, route.Port)
}

// ServiceAddr returns the internal container:port address of one of a
// module's own compose services, for backend code (not other modules)
// that needs to talk to it directly — e.g. the identity client calling
// Authentik's API. Not part of the routes/proxy system: this address is
// never exposed to the edge nginx or a browser.
func (m *Manager) ServiceAddr(moduleID, service string, port int) string {
	return m.upstream(moduleID, Route{Service: service, Port: port})
}

// ContainerName returns the deterministic container name for one of a
// module's own compose services (docker compose's <project>-<service>-1
// naming), for backend code that needs to operate on the container
// directly — e.g. restarting it after rewriting its config out-of-band.
func (m *Manager) ContainerName(moduleID, service string) string {
	return fmt.Sprintf("%s-%s-1", m.projectName(moduleID), service)
}

// VolumeName returns the deterministic name of one of a module's own
// named volumes (docker compose's <project>_<volume-key> naming).
func (m *Manager) VolumeName(moduleID, volumeKey string) string {
	return fmt.Sprintf("%s_%s", m.projectName(moduleID), volumeKey)
}

// GetInstalled returns a single module's stored status and config, or
// ok=false if it has no install record at all.
func (m *Manager) GetInstalled(ctx context.Context, moduleID string) (Status, bool, error) {
	var s Status
	var configJSON []byte
	err := m.db.QueryRow(ctx, `
		SELECT im.module_id, im.status, im.config, im.error_message, im.visibility, mpp.port
		FROM installed_modules im
		LEFT JOIN module_private_ports mpp ON mpp.module_id = im.module_id
		WHERE im.module_id = $1
	`, moduleID,
	).Scan(&s.ModuleID, &s.Status, &configJSON, &s.ErrorMessage, &s.Visibility, &s.PrivatePort)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Status{}, false, nil
		}
		return Status{}, false, fmt.Errorf("query installed_modules: %w", err)
	}
	if err := json.Unmarshal(configJSON, &s.Config); err != nil {
		return Status{}, false, fmt.Errorf("unmarshal config: %w", err)
	}
	return s, true, nil
}

// routeTargets builds the nginx route list for a module. Only the primary
// route (Name == "") is ever private — a named route is always some
// specific secondary thing a module's primary service depends on being
// reachable (not a thing end users browse to directly), so it always
// stays public.
func (m *Manager) routeTargets(ctx context.Context, moduleID string, routes []Route) ([]proxy.RouteTarget, error) {
	targets := make([]proxy.RouteTarget, 0, len(routes))
	for _, r := range routes {
		t := proxy.RouteTarget{
			Name:     r.Name,
			Hostname: m.Hostname(moduleID, r.Name),
			Upstream: m.upstream(moduleID, r),
		}
		if r.Name == "" {
			visibility, err := m.Visibility(ctx, moduleID)
			if err != nil {
				return nil, err
			}
			if visibility == "private" {
				port, err := m.privatePort(ctx, moduleID)
				if err != nil {
					return nil, err
				}
				t.PrivatePort = port
			}
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// Visibility returns "public" or "private" for a module's primary route —
// "private" (the default — see the 0006 migration) means it's reachable
// only through the internal VPN gateway, not the public internet at all.
func (m *Manager) Visibility(ctx context.Context, moduleID string) (string, error) {
	var v string
	err := m.db.QueryRow(ctx, `SELECT visibility FROM installed_modules WHERE module_id = $1`, moduleID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "private", nil
	}
	if err != nil {
		return "", fmt.Errorf("load visibility for %s: %w", moduleID, err)
	}
	return v, nil
}

// SetVisibility persists a module's public/private setting and re-applies
// its nginx route immediately — no reinstall needed, unlike a domain
// change (there's no separately-generated config baked into the module's
// own containers to invalidate here, just which vhost nginx writes).
func (m *Manager) SetVisibility(ctx context.Context, moduleID, visibility string) error {
	if visibility != "public" && visibility != "private" {
		return fmt.Errorf("visibility must be \"public\" or \"private\", got %q", visibility)
	}
	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("unknown module %q", moduleID)
	}
	if _, err := m.db.Exec(ctx, `UPDATE installed_modules SET visibility = $2, updated_at = now() WHERE module_id = $1`, moduleID, visibility); err != nil {
		return fmt.Errorf("save visibility: %w", err)
	}
	if len(manifest.Routes) == 0 {
		return nil
	}
	targets, err := m.routeTargets(ctx, moduleID, manifest.Routes)
	if err != nil {
		return err
	}
	return m.proxy.SetRoutes(ctx, moduleID, targets)
}

// privatePort returns this module's fixed port on the internal gateway's
// address, allocating one (highest existing + 1, starting at 9100) the
// first time it's needed. Stable for the module's lifetime, not
// re-allocated on every visibility toggle, so "private" -> "public" ->
// "private" again doesn't hand out a different port each time.
func (m *Manager) privatePort(ctx context.Context, moduleID string) (int, error) {
	var port int
	err := m.db.QueryRow(ctx, `SELECT port FROM module_private_ports WHERE module_id = $1`, moduleID).Scan(&port)
	if err == nil {
		return port, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("load private port for %s: %w", moduleID, err)
	}
	err = m.db.QueryRow(ctx, `
		INSERT INTO module_private_ports (module_id, port)
		VALUES ($1, (SELECT COALESCE(MAX(port), 9099) + 1 FROM module_private_ports))
		RETURNING port
	`, moduleID).Scan(&port)
	if err != nil {
		return 0, fmt.Errorf("allocate private port for %s: %w", moduleID, err)
	}
	return port, nil
}

// longOp returns a context detached from the inbound HTTP request, with a
// generous fixed timeout instead. Install/Enable/Disable/Uninstall can
// involve slow work (pulling large images on first install) that must
// survive the request's own connection or proxy timing out — otherwise
// that kills the underlying `docker compose` process mid-pull and leaves
// the module half-installed with no record of what happened.
func longOp() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Minute)
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// randomSecretBase64 is for the rare config field that specifically
// expects base64 (e.g. NetBird's datastore encryption key) rather than
// the hex "type: secret" fields use everywhere else.
func randomSecretBase64() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Install validates the submitted config, records the module as
// "installing", and returns immediately — the actual work (which can take
// minutes on a slow connection pulling images for the first time) runs in
// the background. Callers should poll ListStatuses/GetInstalled to see it
// move to "running" or "error". This matters because the previous
// synchronous version tied the whole operation to the inbound HTTP
// request's lifetime: a client or proxy that gave up waiting canceled the
// request context, which killed the underlying `docker compose` process
// mid-pull and left the module stuck half-installed with nothing to show
// for it.
func (m *Manager) Install(ctx context.Context, moduleID string, config map[string]string) error {
	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("unknown module %q", moduleID)
	}
	if manifest.ComposeFile == "" {
		return fmt.Errorf("module %q is not yet available to install", moduleID)
	}
	// A config value containing a newline would inject an extra line into
	// the .env file RenderCompose writes (plain KEY=VALUE, one per line),
	// defining a variable nobody asked for. Rejected outright rather than
	// stripped — silently altering what the admin typed could hide a
	// paste error instead of surfacing it.
	for k, v := range config {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("config value %q can't contain a newline", k)
		}
	}

	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}

	_, err = m.db.Exec(ctx, `
		INSERT INTO installed_modules (module_id, status, config, error_message)
		VALUES ($1, 'installing', '{}', NULL)
		ON CONFLICT (module_id) DO UPDATE
		SET status = 'installing', error_message = NULL, updated_at = now()
	`, moduleID)
	if err != nil {
		done()
		return fmt.Errorf("record installing: %w", err)
	}

	go func() {
		defer done()
		ctx, cancel := longOp()
		defer cancel()
		if err := m.doInstall(ctx, moduleID, manifest, config); err != nil {
			m.markError(moduleID, err)
		}
	}()

	return nil
}

func (m *Manager) doInstall(ctx context.Context, moduleID string, manifest *Manifest, config map[string]string) error {
	resolved := make(map[string]string, len(manifest.ConfigSchema)+2)
	resolved["BASE_DOMAIN"] = m.BaseDomain()
	resolved["MODULE_ID"] = moduleID
	for _, field := range manifest.ConfigSchema {
		v, ok := config[field.Key]
		if !ok || v == "" {
			v = field.Default
		}
		if v == "" && field.Type == "secret" {
			generated, err := randomSecret()
			if err != nil {
				return err
			}
			v = generated
		}
		if v == "" && field.Type == "secret_base64" {
			generated, err := randomSecretBase64()
			if err != nil {
				return err
			}
			v = generated
		}
		resolved[field.Key] = v
	}

	dataDir := m.projectDir(moduleID)
	if _, err := docker.RenderCompose(manifest.Dir, manifest.ComposeFile, dataDir, resolved); err != nil {
		return fmt.Errorf("render compose: %w", err)
	}

	if err := m.docker.ComposeUp(ctx, dataDir, m.projectName(moduleID)); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	if len(manifest.Routes) > 0 {
		targets, err := m.routeTargets(ctx, moduleID, manifest.Routes)
		if err != nil {
			return fmt.Errorf("route module: %w", err)
		}
		if err := m.proxy.SetRoutes(ctx, moduleID, targets); err != nil {
			return fmt.Errorf("route module: %w", err)
		}
	}

	configJSON, err := json.Marshal(resolved)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	_, err = m.db.Exec(ctx, `UPDATE installed_modules SET status = 'running', config = $2, updated_at = now() WHERE module_id = $1`,
		moduleID, configJSON)
	if err != nil {
		return fmt.Errorf("record install: %w", err)
	}

	return nil
}

func (m *Manager) markError(moduleID string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.db.Exec(ctx, `UPDATE installed_modules SET status = 'error', error_message = $2, updated_at = now() WHERE module_id = $1`,
		moduleID, cause.Error())
	if err != nil {
		log.Printf("mark install error for %s (cause: %v): %v", moduleID, cause, err)
	}
}

func (m *Manager) Disable(_ context.Context, moduleID string) error {
	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}
	defer done()

	ctx, cancel := longOp()
	defer cancel()

	if err := m.proxy.RemoveRoutes(ctx, moduleID); err != nil {
		return fmt.Errorf("unroute module: %w", err)
	}
	dataDir := m.projectDir(moduleID)
	if err := m.docker.ComposeDown(ctx, dataDir, m.projectName(moduleID)); err != nil {
		return fmt.Errorf("compose down: %w", err)
	}
	_, err = m.db.Exec(ctx, `UPDATE installed_modules SET status = 'stopped', updated_at = now() WHERE module_id = $1`, moduleID)
	return err
}

func (m *Manager) Enable(_ context.Context, moduleID string) error {
	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}
	defer done()

	ctx, cancel := longOp()
	defer cancel()

	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("unknown module %q", moduleID)
	}

	dataDir := m.projectDir(moduleID)
	if err := m.docker.ComposeUp(ctx, dataDir, m.projectName(moduleID)); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	if len(manifest.Routes) > 0 {
		targets, err := m.routeTargets(ctx, moduleID, manifest.Routes)
		if err != nil {
			return fmt.Errorf("route module: %w", err)
		}
		if err := m.proxy.SetRoutes(ctx, moduleID, targets); err != nil {
			return fmt.Errorf("route module: %w", err)
		}
	}

	_, err = m.db.Exec(ctx, `UPDATE installed_modules SET status = 'running', error_message = NULL, updated_at = now() WHERE module_id = $1`, moduleID)
	return err
}

// Uninstall stops the stack and forgets the install record. Named volumes
// created by the module's own compose file are left intact; only the
// rendered compose/.env under the module's data dir is removed.
func (m *Manager) Uninstall(_ context.Context, moduleID string) error {
	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}
	defer done()

	ctx, cancel := longOp()
	defer cancel()

	if err := m.proxy.RemoveRoutes(ctx, moduleID); err != nil {
		return fmt.Errorf("unroute module: %w", err)
	}
	dataDir := m.projectDir(moduleID)
	if err := m.docker.ComposeDown(ctx, dataDir, m.projectName(moduleID)); err != nil {
		return fmt.Errorf("compose down: %w", err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("remove rendered compose dir: %w", err)
	}
	_, err = m.db.Exec(ctx, `DELETE FROM installed_modules WHERE module_id = $1`, moduleID)
	return err
}

// ListStatuses returns the catalog joined with each module's installed
// status from the database (not_installed if there's no row).
func (m *Manager) ListStatuses(ctx context.Context) ([]Status, error) {
	rows, err := m.db.Query(ctx, `
		SELECT im.module_id, im.status, im.config, im.error_message, im.visibility, mpp.port
		FROM installed_modules im
		LEFT JOIN module_private_ports mpp ON mpp.module_id = im.module_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query installed_modules: %w", err)
	}
	defer rows.Close()

	installed := make(map[string]Status)
	for rows.Next() {
		var s Status
		var configJSON []byte
		if err := rows.Scan(&s.ModuleID, &s.Status, &configJSON, &s.ErrorMessage, &s.Visibility, &s.PrivatePort); err != nil {
			return nil, fmt.Errorf("scan installed_modules: %w", err)
		}
		if err := json.Unmarshal(configJSON, &s.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		installed[s.ModuleID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]Status, 0, len(m.registry.List()))
	for _, manifest := range m.registry.List() {
		if s, ok := installed[manifest.ID]; ok {
			result = append(result, s)
		} else {
			result = append(result, Status{ModuleID: manifest.ID, Status: "not_installed", Config: map[string]string{}})
		}
	}
	return result, nil
}
