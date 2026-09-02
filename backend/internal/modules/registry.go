package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Registry holds the catalog of modules found on disk under ModulesDir.
// Originally read once at startup on the assumption the catalog only
// changes via deploys — but the module registry (see
// backend/internal/registryclient) can now write a brand-new module's
// files, or a newer version's, into ModulesDir at runtime, so Reload lets
// that take effect without a restart. Guarded by a mutex since API
// requests read this concurrently with a reload in progress.
type Registry struct {
	modulesDir string

	mu   sync.RWMutex
	byID map[string]*Manifest
}

func NewRegistry(modulesDir string) (*Registry, error) {
	r := &Registry{modulesDir: modulesDir}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-scans ModulesDir from disk and atomically swaps the catalog.
// Safe to call while other goroutines are reading via Get/List.
func (r *Registry) Reload() error {
	entries, err := os.ReadDir(r.modulesDir)
	if err != nil {
		return fmt.Errorf("read modules dir %s: %w", r.modulesDir, err)
	}

	byID := make(map[string]*Manifest)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(r.modulesDir, e.Name())
		manifestPath := filepath.Join(dir, "manifest.yaml")
		if _, err := os.Stat(manifestPath); err != nil {
			continue // not a module directory
		}
		m, err := LoadManifest(dir)
		if err != nil {
			return err
		}
		byID[m.ID] = m
	}

	r.mu.Lock()
	r.byID = byID
	r.mu.Unlock()
	return nil
}

func (r *Registry) Get(id string) (*Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byID[id]
	return m, ok
}

func (r *Registry) List() []*Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Manifest, 0, len(r.byID))
	for _, m := range r.byID {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
