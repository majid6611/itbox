package modules

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"it-platform/backend/internal/registryclient"
)

// UpdateInfo describes one module's available update, as last computed by
// CheckForUpdates — kept in memory rather than the database, since it's
// entirely derived from the registry index and cheap to recompute; there's
// nothing here worth surviving a restart that the next check wouldn't
// reproduce.
type UpdateInfo struct {
	ModuleID       string `json:"module_id"`
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"` // "" for a module not yet on disk at all
	LatestVersion  string `json:"latest_version"`
	Severity       string `json:"severity"`
	Changelog      string `json:"changelog"`
	// New is true when this module doesn't exist in the local catalog
	// yet at all (a brand-new module, not a newer version of one already
	// there) — the Module Store shows these as newly available rather
	// than as an update badge on an existing row.
	New bool `json:"new"`
	// Status is "updating" while an update is being applied, "failed" if
	// the last attempt didn't take (Message says why, and that the old
	// version is back); empty otherwise.
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// compareVersions compares dotted numeric version strings ("1.2.0" etc.)
// segment by segment, treating a missing trailing segment as 0. Good
// enough for this platform's own manifests, which only ever use plain
// semver-shaped strings — not a full semver implementation (no
// prerelease/build-metadata handling), which would be more machinery than
// anything here actually needs.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// CheckForUpdates fetches the module registry's index and diffs it against
// what's actually on disk, caching the result for GetUpdate/UpdatesSnapshot
// and for the eventual Update(id) call to act on. Returns an empty slice,
// no error, if this deployment has no registry configured at all.
func (m *Manager) CheckForUpdates(ctx context.Context) ([]UpdateInfo, error) {
	if m.registryClient == nil || !m.registryClient.Configured() {
		return nil, nil
	}
	entries, err := m.registryClient.FetchIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch module index: %w", err)
	}

	var found []UpdateInfo
	next := make(map[string]registryclient.IndexEntry, len(entries))
	for _, e := range entries {
		next[e.ID] = e
		current, installed := m.registry.Get(e.ID)
		currentVersion := ""
		if installed {
			currentVersion = current.Version
		}
		if installed && compareVersions(currentVersion, e.LatestVersion) >= 0 {
			continue // already up to date (or ahead, e.g. a local dev build)
		}
		found = append(found, UpdateInfo{
			ModuleID:       e.ID,
			Name:           e.Name,
			CurrentVersion: currentVersion,
			LatestVersion:  e.LatestVersion,
			Severity:       e.Severity,
			Changelog:      e.Changelog,
			New:            !installed,
		})
	}

	m.updatesMu.Lock()
	m.updateEntries = next
	prev := m.updates
	m.updates = make(map[string]UpdateInfo, len(found))
	for _, u := range found {
		if p, ok := prev[u.ModuleID]; ok {
			u.Status, u.Message = p.Status, p.Message
		}
		m.updates[u.ModuleID] = u
	}
	m.updatesMu.Unlock()

	return found, nil
}

// UpdatesSnapshot returns the last CheckForUpdates result without hitting
// the registry again — GET /api/modules calls this on every load, so it
// stays a cheap in-memory read; refreshing is an explicit action.
func (m *Manager) UpdatesSnapshot() map[string]UpdateInfo {
	m.updatesMu.RLock()
	defer m.updatesMu.RUnlock()
	out := make(map[string]UpdateInfo, len(m.updates))
	for k, v := range m.updates {
		out[k] = v
	}
	return out
}

// ApplyUpdate downloads and installs the latest version of a module found
// by the last CheckForUpdates call: verifies the bundle's checksum,
// extracts it over the module's on-disk files, reloads the catalog, and —
// if the module is already installed — re-runs the same compose-up path
// Install uses, with its existing stored config, so a running module
// actually picks up the new version instead of just updating the catalog
// entry. A brand-new module just becomes installable; nothing to bring up
// yet since nothing was running.
func (m *Manager) ApplyUpdate(ctx context.Context, moduleID string) error {
	if m.registryClient == nil || !m.registryClient.Configured() {
		return fmt.Errorf("no module registry is configured for this deployment")
	}

	m.updatesMu.RLock()
	entry, ok := m.updateEntries[moduleID]
	m.updatesMu.RUnlock()
	if !ok {
		return fmt.Errorf("no known update for module %q — try checking for updates again", moduleID)
	}

	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}
	m.setUpdateState(moduleID, "updating", "")

	// Runs on its own context: a browser or proxy giving up on the request
	// must never cut an update off half-way (or skip its rollback).
	go func() {
		defer done()
		ctx, cancel := longOp()
		defer cancel()
		bundle, err := m.registryClient.DownloadBundle(ctx, entry.ID, entry.LatestVersion, entry.SHA256)
		if err != nil {
			err = fmt.Errorf("download module bundle: %w", err)
		} else {
			err = m.applyBundle(ctx, moduleID, bundle)
		}
		if err != nil {
			log.Printf("module update %s: %v", moduleID, err)
			m.setUpdateState(moduleID, "failed", err.Error())
			return
		}
		m.updatesMu.Lock()
		delete(m.updates, moduleID)
		m.updatesMu.Unlock()
	}()
	return nil
}

func (m *Manager) setUpdateState(moduleID, status, message string) {
	m.updatesMu.Lock()
	defer m.updatesMu.Unlock()
	if u, ok := m.updates[moduleID]; ok {
		u.Status, u.Message = status, message
		m.updates[moduleID] = u
	}
}
