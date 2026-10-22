package modules

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"it-platform/backend/internal/db"
)

// updateHealthTimeout is how long a freshly updated module gets to come up
// and stay up before the update is judged failed and rolled back.
const updateHealthTimeout = 90 * time.Second

// applyBundle puts a downloaded module version in place without risking
// what the module already holds:
//
//  1. safety copies first — a database snapshot (for modules that use the
//     shared database), a tar of each of the module's data volumes, and the
//     current module files — and no update if any of them fails;
//  2. unpack and restart on the new version;
//  3. wait for it to actually be healthy;
//  4. if it isn't, put the old files back and start the old version.
//
// The database is deliberately NOT restored on a rollback: that would
// erase whatever users saved since the update began. Migrations only add
// (see TestMigrationsDontDestroyData), so the old version runs fine on the
// newer schema. Restoring a snapshot stays a manual, explicit decision.
func (m *Manager) applyBundle(ctx context.Context, moduleID string, bundle []byte) error {
	destDir := filepath.Join(m.registry.modulesDir, moduleID)
	old, hadOld := m.registry.Get(moduleID)
	status, installed, err := m.GetInstalled(ctx, moduleID)
	if err != nil {
		return fmt.Errorf("check install status: %w", err)
	}
	live := hadOld && installed && status.Status != "not_installed"

	var keepDir string
	if live {
		if old.NeedsDatabase {
			if _, err := db.Snapshot(ctx, m.databaseURL, m.SnapshotDir, "before-update-"+moduleID); err != nil {
				return fmt.Errorf("not updating: couldn't take a safety snapshot of the database first: %w", err)
			}
		}
		if err := m.snapshotVolumes(ctx, moduleID); err != nil {
			return fmt.Errorf("not updating: couldn't save a copy of the module's data first: %w", err)
		}
		keepDir = filepath.Join(m.SnapshotDir, "module-files", moduleID, old.Version)
		os.RemoveAll(keepDir)
		if err := copyDir(destDir, keepDir); err != nil {
			return fmt.Errorf("not updating: couldn't keep a copy of the current version: %w", err)
		}
	}

	if err := extractBundle(bundle, destDir); err != nil {
		if keepDir != "" {
			m.restoreFiles(destDir, keepDir)
		}
		return fmt.Errorf("extract module bundle: %w", err)
	}
	if err := m.registry.Reload(); err != nil {
		return fmt.Errorf("reload catalog: %w", err)
	}
	manifest, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("module %q missing from catalog after extracting its bundle", moduleID)
	}
	if !live {
		return nil // a newly fetched module nobody has installed yet
	}

	err = m.doInstall(ctx, moduleID, manifest, status.Config)
	if err == nil {
		err = m.WaitHealthy(ctx, moduleID, updateHealthTimeout)
	}
	if err == nil {
		return nil
	}

	// Failed: back to the version that was working.
	m.restoreFiles(destDir, keepDir)
	if rerr := m.registry.Reload(); rerr != nil {
		return fmt.Errorf("update to %s failed (%v) and the old version couldn't be reloaded: %w", manifest.Version, err, rerr)
	}
	restored, ok := m.registry.Get(moduleID)
	if !ok {
		return fmt.Errorf("update to %s failed (%v) and the old version is missing", manifest.Version, err)
	}
	if rerr := m.doInstall(ctx, moduleID, restored, status.Config); rerr != nil {
		m.markError(moduleID, rerr)
		return fmt.Errorf("update to %s failed (%v) and starting version %s again also failed: %w", manifest.Version, err, old.Version, rerr)
	}
	if rerr := m.WaitHealthy(ctx, moduleID, updateHealthTimeout); rerr != nil {
		return fmt.Errorf("update to %s failed (%v); restored version %s but it isn't healthy either: %w", manifest.Version, err, old.Version, rerr)
	}
	return fmt.Errorf("update to %s failed and was rolled back to %s; no data was changed: %w", manifest.Version, old.Version, err)
}

func (m *Manager) restoreFiles(destDir, keepDir string) {
	os.RemoveAll(destDir)
	_ = copyDir(keepDir, destDir)
}

// snapshotVolumes saves a tar of each volume the module opted in to
// backing up, keeping the most recent few.
func (m *Manager) snapshotVolumes(ctx context.Context, moduleID string) error {
	vols := m.BackupVolumes(moduleID)
	if len(vols) == 0 {
		return nil
	}
	dir := filepath.Join(m.SnapshotDir, "volumes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	for key, volume := range vols {
		if err := m.docker.TarVolumeToFile(ctx, volume, filepath.Join(dir, fmt.Sprintf("%s-%s-%s.tar.gz", stamp, moduleID, key))); err != nil {
			return err
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))
	sort.Strings(files)
	for len(files) > 10 {
		os.Remove(files[0])
		files = files[1:]
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
