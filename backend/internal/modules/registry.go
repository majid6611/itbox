package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Registry holds the catalog of modules found on disk under ModulesDir.
// It is read once at startup; the catalog is expected to change only via
// deploys, not at runtime.
type Registry struct {
	byID map[string]*Manifest
}

func NewRegistry(modulesDir string) (*Registry, error) {
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, fmt.Errorf("read modules dir %s: %w", modulesDir, err)
	}

	byID := make(map[string]*Manifest)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(modulesDir, e.Name())
		manifestPath := filepath.Join(dir, "manifest.yaml")
		if _, err := os.Stat(manifestPath); err != nil {
			continue // not a module directory
		}
		m, err := LoadManifest(dir)
		if err != nil {
			return nil, err
		}
		byID[m.ID] = m
	}

	return &Registry{byID: byID}, nil
}

func (r *Registry) Get(id string) (*Manifest, bool) {
	m, ok := r.byID[id]
	return m, ok
}

func (r *Registry) List() []*Manifest {
	list := make([]*Manifest, 0, len(r.byID))
	for _, m := range r.byID {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
