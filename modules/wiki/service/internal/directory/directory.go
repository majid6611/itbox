// Package directory looks up which LDAP group an employee belongs to, for
// the wiki's per-page permission checks. It gets the ldap-openldap
// module's connection details (address, bind DN, admin password) by
// reading installed_modules directly — the same shared Postgres core
// itself reads that table from — rather than calling back into core over
// HTTP. Read-only: this module never creates/modifies LDAP users or
// groups, only checks membership.
package directory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-ldap/ldap/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// clientFromDB returns an LDAP client for the ldap-openldap module, or
// available=false if it isn't installed and running.
func clientFromDB(ctx context.Context, db *pgxpool.Pool) (addr, bindDN, password, baseDN string, available bool, err error) {
	var status string
	var configJSON []byte
	err = db.QueryRow(ctx, `SELECT status, config FROM installed_modules WHERE module_id = 'ldap-openldap'`).Scan(&status, &configJSON)
	if err != nil {
		return "", "", "", "", false, nil // not installed
	}
	if status != "running" {
		return "", "", "", "", false, nil
	}
	var cfg struct {
		BaseDN        string `json:"base_dn"`
		AdminPassword string `json:"admin_password"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return "", "", "", "", false, fmt.Errorf("parse ldap-openldap config: %w", err)
	}
	// Fixed internal hostname:port — every module reaches another module's
	// service the same way core does, via Docker's embedded DNS for the
	// <project>-<service>-1 container name on the shared edge network.
	return "itp-ldap-openldap-openldap-1:389", fmt.Sprintf("uid=admin,%s", cfg.BaseDN), cfg.AdminPassword, cfg.BaseDN, true, nil
}

// GroupFor returns the LDAP group the given username belongs to, or "" if
// none (not an error — matches core's own employeeGroup, which degrades to
// "matches nothing" rather than failing a page load over it).
func GroupFor(ctx context.Context, db *pgxpool.Pool, username string) (string, error) {
	addr, bindDN, password, baseDN, available, err := clientFromDB(ctx, db)
	if err != nil || !available {
		return "", err
	}

	conn, err := ldap.DialURL("ldap://" + addr)
	if err != nil {
		return "", fmt.Errorf("dial ldap: %w", err)
	}
	defer conn.Close()
	if err := conn.Bind(bindDN, password); err != nil {
		return "", fmt.Errorf("bind: %w", err)
	}

	req := ldap.NewSearchRequest(
		"ou=groups,"+baseDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=groupOfNames)", []string{"cn", "member"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return "", fmt.Errorf("search groups: %w", err)
	}

	userDN := fmt.Sprintf("uid=%s,ou=people,%s", ldap.EscapeDN(username), baseDN)
	for _, entry := range res.Entries {
		for _, member := range entry.GetAttributeValues("member") {
			if member == userDN {
				return entry.GetAttributeValue("cn"), nil
			}
		}
	}
	return "", nil
}
