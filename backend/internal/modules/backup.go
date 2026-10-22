package modules

import (
	"context"
	"encoding/json"
	"fmt"
)

// BackupVolumes returns the full docker volume names (keyed by the
// compose volume key) a module opted in to backing up via its manifest's
// backup_volumes. Empty if the module is unknown or declares none.
func (m *Manager) BackupVolumes(moduleID string) map[string]string {
	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(manifest.BackupVolumes))
	for _, key := range manifest.BackupVolumes {
		out[key] = m.VolumeName(moduleID, key)
	}
	return out
}

// InstalledIDs lists every module with an install record, whatever its
// status.
func (m *Manager) InstalledIDs(ctx context.Context) ([]string, error) {
	rows, err := m.db.Query(ctx, `SELECT module_id FROM installed_modules ORDER BY module_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Reinstall re-renders a module from its stored config and brings it up,
// waiting for it to finish — unlike Install, which returns immediately.
// Used after a platform restore, where the install records came back from
// a backup but the module's files and containers don't exist on this host.
func (m *Manager) Reinstall(ctx context.Context, moduleID string) error {
	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("unknown module %q", moduleID)
	}
	done, err := m.beginOp(moduleID)
	if err != nil {
		return err
	}
	defer done()

	var raw []byte
	if err := m.db.QueryRow(ctx, `SELECT config FROM installed_modules WHERE module_id = $1`, moduleID).Scan(&raw); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	config := map[string]string{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return m.doInstall(ctx, moduleID, manifest, config)
}

// ReloadSettings re-reads the settings NewManager cached at startup, for
// when the database changed underneath the running process (a restore).
func (m *Manager) ReloadSettings(ctx context.Context) error {
	var domain, theme string
	if err := m.db.QueryRow(ctx, `SELECT base_domain, theme FROM platform_settings WHERE id = true`).Scan(&domain, &theme); err != nil {
		return err
	}
	m.baseDomainMu.Lock()
	m.baseDomain = domain
	m.baseDomainMu.Unlock()
	m.themeMu.Lock()
	m.theme = theme
	m.themeMu.Unlock()
	return nil
}

// ProjectName is the docker compose project a module runs under.
func (m *Manager) ProjectName(moduleID string) string { return m.projectName(moduleID) }

// DataDir is where module files are rendered — on the host's data disk.
func (m *Manager) DataDir() string { return m.dataDir }
