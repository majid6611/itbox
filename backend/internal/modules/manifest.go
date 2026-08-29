package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ConfigField struct {
	Key     string `yaml:"key" json:"key"`
	Label   string `yaml:"label" json:"label"`
	Type    string `yaml:"type" json:"type"`
	Default string `yaml:"default" json:"default"`
	// Hidden fields are never shown in the install form. Combined with
	// Type: "secret" and no default, the value is generated randomly at
	// install time instead of asked for (e.g. internal RPC/admin tokens).
	Hidden bool `yaml:"hidden,omitempty" json:"hidden,omitempty"`
}

type SoftDependency struct {
	ID          string `yaml:"id" json:"id"`
	Integration string `yaml:"integration" json:"integration"`
}

// Route describes a service/port of the module's own compose file to
// expose through the edge nginx. The primary route (Name == "") is
// published at <module id>.<base domain>; any additional named routes at
// <name>.<module id>.<base domain>. Modules with no web UI declare none.
type Route struct {
	Name    string `yaml:"name,omitempty" json:"name,omitempty"`
	Service string `yaml:"service" json:"service"`
	Port    int    `yaml:"port" json:"port"`
}

type Manifest struct {
	ID               string           `yaml:"id" json:"id"`
	Name             string           `yaml:"name" json:"name"`
	Description      string           `yaml:"description" json:"description"`
	Category         string           `yaml:"category" json:"category"`
	Version          string           `yaml:"version" json:"version"`
	ComposeFile      string           `yaml:"compose_file" json:"-"`
	ConfigSchema     []ConfigField    `yaml:"config_schema" json:"config_schema"`
	SoftDependencies []SoftDependency `yaml:"soft_dependencies" json:"soft_dependencies"`
	Routes           []Route          `yaml:"routes" json:"routes,omitempty"`

	// InternalPanel is a path in this platform's own frontend (e.g.
	// "/users") shown as a "Manage" link when the module is running —
	// for modules whose admin UI is a page we built ourselves rather
	// than the module's own web UI (which may not exist, or may be one
	// we've deliberately chosen not to expose).
	InternalPanel string `yaml:"internal_panel,omitempty" json:"internal_panel,omitempty"`

	// Available is true once the module has a compose file to install.
	// Modules without one are catalog stubs ("coming soon").
	Available bool `yaml:"-" json:"available"`

	// Dir is the absolute path to the module's catalog directory. Not part
	// of the YAML; set by the registry after loading.
	Dir string `yaml:"-" json:"-"`
}

func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, "manifest.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.ID == "" {
		return nil, fmt.Errorf("manifest %s: id is required", path)
	}

	m.Available = m.ComposeFile != ""
	m.Dir = dir
	return &m, nil
}
