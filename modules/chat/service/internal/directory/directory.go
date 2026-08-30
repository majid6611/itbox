// Package directory reads LDAP directly for what chat needs: which group
// an employee belongs to (their group is their channel), a group's member
// list (who a channel message is actually routed to), and the full list of
// employees (the DM picker). Gets the ldap-openldap module's connection
// details by reading
// installed_modules from the same shared Postgres core itself reads that
// table from, rather than calling back into core over HTTP — same pattern
// as the wiki module's own directory package. Read-only.
package directory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-ldap/ldap/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// connect returns a bound LDAP connection and the directory's base DN, or
// available=false if ldap-openldap isn't installed and running. Caller
// must Close the connection.
func connect(ctx context.Context, db *pgxpool.Pool) (conn *ldap.Conn, baseDN string, available bool, err error) {
	var status string
	var configJSON []byte
	err = db.QueryRow(ctx, `SELECT status, config FROM installed_modules WHERE module_id = 'ldap-openldap'`).Scan(&status, &configJSON)
	if err != nil {
		return nil, "", false, nil // not installed
	}
	if status != "running" {
		return nil, "", false, nil
	}
	var cfg struct {
		BaseDN        string `json:"base_dn"`
		AdminPassword string `json:"admin_password"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, "", false, fmt.Errorf("parse ldap-openldap config: %w", err)
	}

	// Fixed internal hostname:port — every module reaches another module's
	// service the same way core does, via Docker's embedded DNS for the
	// <project>-<service>-1 container name on the shared edge network.
	c, err := ldap.DialURL("ldap://itp-ldap-openldap-openldap-1:389")
	if err != nil {
		return nil, "", false, fmt.Errorf("dial ldap: %w", err)
	}
	if err := c.Bind(fmt.Sprintf("uid=admin,%s", cfg.BaseDN), cfg.AdminPassword); err != nil {
		c.Close()
		return nil, "", false, fmt.Errorf("bind: %w", err)
	}
	return c, cfg.BaseDN, true, nil
}

// GroupFor returns the LDAP group the given username belongs to, or "" if
// none (not an error — degrades to "matches nothing" rather than failing).
func GroupFor(ctx context.Context, db *pgxpool.Pool, username string) (string, error) {
	conn, baseDN, available, err := connect(ctx, db)
	if err != nil || !available {
		return "", err
	}
	defer conn.Close()

	groups, err := searchGroups(conn, baseDN)
	if err != nil {
		return "", err
	}
	userDN := fmt.Sprintf("uid=%s,ou=people,%s", ldap.EscapeDN(username), baseDN)
	for _, g := range groups {
		for _, member := range g.members {
			if member == userDN {
				return g.name, nil
			}
		}
	}
	return "", nil
}

// ListUsers returns every employee's username — the DM target picker.
func ListUsers(ctx context.Context, db *pgxpool.Pool) ([]string, error) {
	conn, baseDN, available, err := connect(ctx, db)
	if err != nil || !available {
		return nil, err
	}
	defer conn.Close()

	req := ldap.NewSearchRequest(
		"ou=people,"+baseDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=inetOrgPerson)", []string{"uid"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	usernames := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		usernames = append(usernames, e.GetAttributeValue("uid"))
	}
	return usernames, nil
}

// MembersOf returns the usernames belonging to the named LDAP group — used
// to route a group channel's live messages only to people who actually
// belong to it, the same restriction the read/write REST endpoints enforce.
func MembersOf(ctx context.Context, db *pgxpool.Pool, groupName string) ([]string, error) {
	conn, baseDN, available, err := connect(ctx, db)
	if err != nil || !available {
		return nil, err
	}
	defer conn.Close()

	groups, err := searchGroups(conn, baseDN)
	if err != nil {
		return nil, err
	}
	var memberDNs map[string]bool
	for _, g := range groups {
		if g.name == groupName {
			memberDNs = make(map[string]bool, len(g.members))
			for _, dn := range g.members {
				memberDNs[dn] = true
			}
			break
		}
	}
	if len(memberDNs) == 0 {
		return nil, nil
	}

	req := ldap.NewSearchRequest(
		"ou=people,"+baseDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=inetOrgPerson)", []string{"uid"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	var usernames []string
	for _, e := range res.Entries {
		if memberDNs[e.DN] {
			usernames = append(usernames, e.GetAttributeValue("uid"))
		}
	}
	return usernames, nil
}

type ldapGroup struct {
	name    string
	members []string
}

func searchGroups(conn *ldap.Conn, baseDN string) ([]ldapGroup, error) {
	req := ldap.NewSearchRequest(
		"ou=groups,"+baseDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=groupOfNames)", []string{"cn", "member"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search groups: %w", err)
	}
	groups := make([]ldapGroup, 0, len(res.Entries))
	for _, e := range res.Entries {
		groups = append(groups, ldapGroup{name: e.GetAttributeValue("cn"), members: e.GetAttributeValues("member")})
	}
	return groups, nil
}
