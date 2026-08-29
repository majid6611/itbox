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
	err := m.db.QueryRow(ctx,
		`SELECT module_id, status, config, error_message FROM installed_modules WHERE module_id = $1`, moduleID,
	).Scan(&s.ModuleID, &s.Status, &configJSON, &s.ErrorMessage)
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

func (m *Manager) routeTargets(moduleID string, routes []Route) []proxy.RouteTarget {
	targets := make([]proxy.RouteTarget, 0, len(routes))
	for _, r := range routes {
		targets = append(targets, proxy.RouteTarget{
			Name:     r.Name,
			Hostname: m.Hostname(moduleID, r.Name),
			Upstream: m.upstream(moduleID, r),
		})
	}
	return targets
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
		if err := m.proxy.SetRoutes(ctx, moduleID, m.routeTargets(moduleID, manifest.Routes)); err != nil {
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
		if err := m.proxy.SetRoutes(ctx, moduleID, m.routeTargets(moduleID, manifest.Routes)); err != nil {
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
	rows, err := m.db.Query(ctx, `SELECT module_id, status, config, error_message FROM installed_modules`)
	if err != nil {
		return nil, fmt.Errorf("query installed_modules: %w", err)
	}
	defer rows.Close()

	installed := make(map[string]Status)
	for rows.Next() {
		var s Status
		var configJSON []byte
		if err := rows.Scan(&s.ModuleID, &s.Status, &configJSON, &s.ErrorMessage); err != nil {
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
