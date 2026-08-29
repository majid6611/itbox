package webdavfs

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"
)

type VolumeRW interface {
	ReadVolumeFile(ctx context.Context, volume, path string) (string, error)
	WriteVolumeFile(ctx context.Context, volume, path, content string) error
	RestartContainer(ctx context.Context, containerName string) error
}

type configRule struct {
	Path        string `yaml:"path"`
	Permissions string `yaml:"permissions"`
}

type configUser struct {
	Username    string       `yaml:"username"`
	Password    string       `yaml:"password"`
	Permissions string       `yaml:"permissions"`
	Rules       []configRule `yaml:"rules,omitempty"`
}

// config mirrors exactly what modules/fileshare-webdav/docker-compose.yaml
// generates at install time. WebDAV has no live API to manage its users —
// config.yml is the only source of truth — so keeping an LDAP user's
// WebDAV login and folder access in sync means reading this file, editing
// it, writing it back, and restarting the container to pick it up.
type config struct {
	Address     string       `yaml:"address"`
	Port        int          `yaml:"port"`
	TLS         bool         `yaml:"tls"`
	Prefix      string       `yaml:"prefix"`
	BehindProxy bool         `yaml:"behindProxy"`
	Directory   string       `yaml:"directory"`
	Permissions string       `yaml:"permissions"`
	Users       []configUser `yaml:"users"`
}

const sharedFolder = "shared"

func readConfig(ctx context.Context, docker VolumeRW, volume string) (*config, error) {
	raw, err := docker.ReadVolumeFile(ctx, volume, "config.yml")
	if err != nil {
		return nil, fmt.Errorf("read webdav config: %w", err)
	}
	var cfg config
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse webdav config: %w", err)
	}
	return &cfg, nil
}

func writeConfig(ctx context.Context, docker VolumeRW, volume, containerName string, cfg *config) error {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode webdav config: %w", err)
	}
	if err := docker.WriteVolumeFile(ctx, volume, "config.yml", string(out)); err != nil {
		return fmt.Errorf("write webdav config: %w", err)
	}
	if err := docker.RestartContainer(ctx, containerName); err != nil {
		return fmt.Errorf("restart webdav: %w", err)
	}
	return nil
}

// rulesFor confines a user to their own folder, their group's folder, and
// the shared folder — explicitly denying every other known user's and
// group's folder ("fully private" rather than relying on a permissive
// global default). Root gets an explicit read-only rule so connecting to
// the bare WebDAV address still works and shows folder names (a normal
// shared-drive feel for non-technical users) — everything under those
// other names is still denied by the more specific rules below (more
// specific path prefixes win — verified empirically, not assumed).
func rulesFor(self string, allUsernames []string, myGroup string, allGroups []string) []configRule {
	rules := []configRule{
		{Path: "/", Permissions: "R"},
		{Path: "/" + self + "/", Permissions: "CRUD"},
		{Path: "/" + sharedFolder + "/", Permissions: "CRUD"},
	}
	if myGroup != "" {
		rules = append(rules, configRule{Path: "/" + myGroup + "/", Permissions: "CRUD"})
	}
	for _, u := range allUsernames {
		if u == self {
			continue
		}
		rules = append(rules, configRule{Path: "/" + u + "/", Permissions: "none"})
	}
	for _, g := range allGroups {
		if g == myGroup {
			continue
		}
		rules = append(rules, configRule{Path: "/" + g + "/", Permissions: "none"})
	}
	return rules
}

func applyRules(cfg *config, allLDAPUsernames []string, groupOf map[string]string, allGroups []string) {
	ldapSet := make(map[string]bool, len(allLDAPUsernames))
	for _, u := range allLDAPUsernames {
		ldapSet[u] = true
	}
	for i := range cfg.Users {
		if !ldapSet[cfg.Users[i].Username] {
			continue // e.g. the module's own admin account — not LDAP-managed, leave alone
		}
		cfg.Users[i].Permissions = "none"
		cfg.Users[i].Rules = rulesFor(cfg.Users[i].Username, allLDAPUsernames, groupOf[cfg.Users[i].Username], allGroups)
	}
}

// SyncUser adds or updates one user's WebDAV login to match their current
// LDAP password, then rebuilds folder-access rules for every LDAP-managed
// user (allLDAPUsernames/groupOf/allGroups — the full current directory
// state, not just this one) so each person stays confined to their own
// folder, their group's folder, and "shared". One read-modify-write and
// one restart, regardless of how many users are affected.
func SyncUser(ctx context.Context, docker VolumeRW, volume, containerName, username, password string, allLDAPUsernames []string, groupOf map[string]string, allGroups []string) error {
	cfg, err := readConfig(ctx, docker, volume)
	if err != nil {
		return err
	}

	found := false
	for i, u := range cfg.Users {
		if u.Username == username {
			cfg.Users[i].Password = password
			found = true
			break
		}
	}
	if !found {
		cfg.Users = append(cfg.Users, configUser{Username: username, Password: password, Permissions: "none"})
	}

	applyRules(cfg, allLDAPUsernames, groupOf, allGroups)

	return writeConfig(ctx, docker, volume, containerName, cfg)
}

// RebuildRules re-applies folder-access rules for every LDAP-managed user
// without changing any passwords — for when group structure changes
// (a group is created/deleted, or someone moves to a different group) but
// no particular user's login is being touched.
func RebuildRules(ctx context.Context, docker VolumeRW, volume, containerName string, allLDAPUsernames []string, groupOf map[string]string, allGroups []string) error {
	cfg, err := readConfig(ctx, docker, volume)
	if err != nil {
		return err
	}
	applyRules(cfg, allLDAPUsernames, groupOf, allGroups)
	return writeConfig(ctx, docker, volume, containerName, cfg)
}

// RemoveUser removes a user's WebDAV login, if present, and restarts the
// container. Their folder/files are left alone — deleting an account
// revokes access, it doesn't destroy their data. Other users keep a
// (now-inert) deny rule for the removed folder; harmless, not worth a
// second rebuild pass to clean up.
func RemoveUser(ctx context.Context, docker VolumeRW, volume, containerName, username string) error {
	cfg, err := readConfig(ctx, docker, volume)
	if err != nil {
		return err
	}

	kept := cfg.Users[:0]
	removed := false
	for _, u := range cfg.Users {
		if u.Username == username {
			removed = true
			continue
		}
		kept = append(kept, u)
	}
	if !removed {
		return nil
	}
	cfg.Users = kept

	return writeConfig(ctx, docker, volume, containerName, cfg)
}
