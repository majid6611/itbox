// Package directory talks to the OpenLDAP module directly over the LDAP
// protocol (no REST API layer — LDAP is the protocol) to manage company
// user accounts, so the platform's own Users panel is the only place an
// admin needs to go.
package directory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

type Client struct {
	addr     string // host:port
	bindDN   string
	password string
	baseDN   string
}

func NewClient(addr, bindDN, password, baseDN string) *Client {
	return &Client{addr: addr, bindDN: bindDN, password: password, baseDN: baseDN}
}

func (c *Client) peopleOU() string { return "ou=people," + c.baseDN }

func (c *Client) userDN(username string) string {
	return fmt.Sprintf("uid=%s,%s", username, c.peopleOU())
}

func (c *Client) connect() (*ldap.Conn, error) {
	conn, err := ldap.DialURL("ldap://" + c.addr)
	if err != nil {
		return nil, fmt.Errorf("dial ldap: %w", err)
	}
	if err := conn.Bind(c.bindDN, c.password); err != nil {
		conn.Close()
		return nil, fmt.Errorf("bind: %w", err)
	}
	return conn, nil
}

// ensurePeopleOU creates the ou=people container if it doesn't exist yet.
// OpenLDAP starts with just the base DN + admin — nothing under it.
func (c *Client) ensurePeopleOU(conn *ldap.Conn) error {
	req := ldap.NewAddRequest(c.peopleOU(), nil)
	req.Attribute("objectClass", []string{"organizationalUnit"})
	req.Attribute("ou", []string{"people"})
	err := conn.Add(req)
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultEntryAlreadyExists) {
		return fmt.Errorf("ensure people OU: %w", err)
	}
	return nil
}

type User struct {
	Username string
	Email    string
	Name     string
}

func (c *Client) ListUsers() ([]User, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := c.ensurePeopleOU(conn); err != nil {
		return nil, err
	}

	req := ldap.NewSearchRequest(
		c.peopleOU(), ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=inetOrgPerson)", []string{"uid", "mail", "cn"}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}

	users := make([]User, 0, len(res.Entries))
	for _, e := range res.Entries {
		users = append(users, User{
			Username: e.GetAttributeValue("uid"),
			Email:    e.GetAttributeValue("mail"),
			Name:     e.GetAttributeValue("cn"),
		})
	}
	return users, nil
}

// VerifyPassword checks a user's own password by binding as them
// directly — the standard way to "log in" via LDAP, distinct from the
// admin bind (c.bindDN/c.password) used everywhere else in this file for
// directory lookups and management. Uses its own connection, never the
// admin one, so a failed login can't disturb anything else.
func (c *Client) VerifyPassword(username, password string) (bool, error) {
	conn, err := ldap.DialURL("ldap://" + c.addr)
	if err != nil {
		return false, fmt.Errorf("dial ldap: %w", err)
	}
	defer conn.Close()

	if err := conn.Bind(c.userDN(username), password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return false, nil
		}
		return false, fmt.Errorf("bind: %w", err)
	}
	return true, nil
}

// MinPasswordLength is the only password rule this platform enforces —
// deliberately just a length floor, no complexity requirements.
const MinPasswordLength = 8

// ValidatePassword rejects a password that's too short. An empty password
// is always allowed through here — that's the "auto-generate one" signal
// used throughout the user/password APIs, not a real password to check.
func ValidatePassword(password string) error {
	if password != "" && len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// CreateUser creates the user and returns the password actually used
// (either the one supplied, or a freshly generated one if left blank).
func (c *Client) CreateUser(username, email, name, password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}

	conn, err := c.connect()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if err := c.ensurePeopleOU(conn); err != nil {
		return "", err
	}

	if password == "" {
		password, err = randomPassword()
		if err != nil {
			return "", err
		}
	}

	sn := name
	if sn == "" {
		sn = username
	}
	cn := name
	if cn == "" {
		cn = username
	}

	req := ldap.NewAddRequest(c.userDN(username), nil)
	req.Attribute("objectClass", []string{"inetOrgPerson"})
	req.Attribute("uid", []string{username})
	req.Attribute("cn", []string{cn})
	req.Attribute("sn", []string{sn})
	if email != "" {
		req.Attribute("mail", []string{email})
	}
	req.Attribute("userPassword", []string{password})

	if err := conn.Add(req); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return password, nil
}

// UpdateUser replaces name/email if non-empty; pass "" to leave a field
// unchanged.
func (c *Client) UpdateUser(username, email, name string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	req := ldap.NewModifyRequest(c.userDN(username), nil)
	if name != "" {
		req.Replace("cn", []string{name})
		req.Replace("sn", []string{name})
	}
	if email != "" {
		req.Replace("mail", []string{email})
	}
	if len(req.Changes) == 0 {
		return nil
	}
	if err := conn.Modify(req); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// ResetPassword sets a new password for the user, generating a random one
// if newPassword is empty, and returns the password that was set.
func (c *Client) ResetPassword(username, newPassword string) (string, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return "", err
	}

	conn, err := c.connect()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if newPassword == "" {
		newPassword, err = randomPassword()
		if err != nil {
			return "", err
		}
	}

	req := ldap.NewModifyRequest(c.userDN(username), nil)
	req.Replace("userPassword", []string{newPassword})
	if err := conn.Modify(req); err != nil {
		return "", fmt.Errorf("reset password: %w", err)
	}
	return newPassword, nil
}

func (c *Client) DeleteUser(username string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	req := ldap.NewDelRequest(c.userDN(username), nil)
	if err := conn.Del(req); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func randomPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return hex.EncodeToString(b), nil
}
