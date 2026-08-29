package directory

import (
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

func (c *Client) groupsOU() string { return "ou=groups," + c.baseDN }

func (c *Client) groupDN(name string) string {
	return fmt.Sprintf("cn=%s,%s", name, c.groupsOU())
}

// ensureGroupsOU creates the ou=groups container if it doesn't exist yet.
func (c *Client) ensureGroupsOU(conn *ldap.Conn) error {
	req := ldap.NewAddRequest(c.groupsOU(), nil)
	req.Attribute("objectClass", []string{"organizationalUnit"})
	req.Attribute("ou", []string{"groups"})
	err := conn.Add(req)
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultEntryAlreadyExists) {
		return fmt.Errorf("ensure groups OU: %w", err)
	}
	return nil
}

type Group struct {
	Name    string
	Members []string // usernames, not DNs
}

// ListGroups returns every group and its members. The bind DN is used as
// a placeholder member on empty groups (groupOfNames requires at least
// one) and is filtered out of the reported member list — it's plumbing,
// not a real user.
func (c *Client) ListGroups() ([]Group, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := c.ensureGroupsOU(conn); err != nil {
		return nil, err
	}

	req := ldap.NewSearchRequest(
		c.groupsOU(), ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=groupOfNames)", []string{"cn", "member"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search groups: %w", err)
	}

	groups := make([]Group, 0, len(res.Entries))
	for _, e := range res.Entries {
		groups = append(groups, Group{
			Name:    e.GetAttributeValue("cn"),
			Members: c.usernamesFromDNs(e.GetAttributeValues("member")),
		})
	}
	return groups, nil
}

func (c *Client) usernamesFromDNs(dns []string) []string {
	names := make([]string, 0, len(dns))
	for _, dn := range dns {
		if dn == c.bindDN {
			continue // the placeholder used to satisfy groupOfNames' member requirement
		}
		parsed, err := ldap.ParseDN(dn)
		if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
			continue
		}
		names = append(names, parsed.RDNs[0].Attributes[0].Value)
	}
	return names
}

// CreateGroup creates an empty group (seeded with the placeholder member
// groupOfNames requires).
func (c *Client) CreateGroup(name string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := c.ensureGroupsOU(conn); err != nil {
		return err
	}

	req := ldap.NewAddRequest(c.groupDN(name), nil)
	req.Attribute("objectClass", []string{"groupOfNames"})
	req.Attribute("cn", []string{name})
	req.Attribute("member", []string{c.bindDN})
	if err := conn.Add(req); err != nil {
		return fmt.Errorf("create group: %w", err)
	}
	return nil
}

func (c *Client) DeleteGroup(name string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	req := ldap.NewDelRequest(c.groupDN(name), nil)
	if err := conn.Del(req); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

func (c *Client) groupMemberDNs(conn *ldap.Conn, group string) ([]string, error) {
	req := ldap.NewSearchRequest(
		c.groupDN(group), ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=groupOfNames)", []string{"member"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("read group %s: %w", group, err)
	}
	if len(res.Entries) == 0 {
		return nil, fmt.Errorf("group %q not found", group)
	}
	return res.Entries[0].GetAttributeValues("member"), nil
}

func (c *Client) setGroupMemberDNs(conn *ldap.Conn, group string, memberDNs []string) error {
	if len(memberDNs) == 0 {
		memberDNs = []string{c.bindDN} // groupOfNames can't be empty
	}
	req := ldap.NewModifyRequest(c.groupDN(group), nil)
	req.Replace("member", memberDNs)
	if err := conn.Modify(req); err != nil {
		return fmt.Errorf("update group %s: %w", group, err)
	}
	return nil
}

// AddUserToGroup adds a user as a member, dropping the placeholder entry
// if this is the group's first real member.
func (c *Client) AddUserToGroup(username, group string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	current, err := c.groupMemberDNs(conn, group)
	if err != nil {
		return err
	}

	memberDN := c.userDN(username)
	filtered := make([]string, 0, len(current)+1)
	found := false
	for _, dn := range current {
		if dn == c.bindDN {
			continue
		}
		if dn == memberDN {
			found = true
		}
		filtered = append(filtered, dn)
	}
	if !found {
		filtered = append(filtered, memberDN)
	}

	return c.setGroupMemberDNs(conn, group, filtered)
}

// RemoveUserFromGroup removes a user from a group, if they're in it.
func (c *Client) RemoveUserFromGroup(username, group string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	current, err := c.groupMemberDNs(conn, group)
	if err != nil {
		return err
	}

	memberDN := c.userDN(username)
	filtered := make([]string, 0, len(current))
	for _, dn := range current {
		if dn == memberDN {
			continue
		}
		filtered = append(filtered, dn)
	}

	return c.setGroupMemberDNs(conn, group, filtered)
}
