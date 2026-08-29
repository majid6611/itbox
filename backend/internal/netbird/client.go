// Package netbird wraps NetBird's REST management API — no client library
// needed, it's plain JSON over HTTP. We never touch NetBird's own
// dashboard; this is the only thing that talks to it.
package netbird

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	baseURL string
	token   string // Personal Access Token, empty until Setup or SetToken
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

func (c *Client) SetToken(token string) {
	c.token = token
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Token "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("netbird API %s %s: %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Setup performs the one-time, pre-first-account bootstrap and returns a
// Personal Access Token — the only way to get initial API access
// non-interactively. Errors (including "already completed", HTTP 412) if
// called more than once.
func (c *Client) Setup(ctx context.Context, email, password, name string) (pat string, err error) {
	var out struct {
		PersonalAccessToken string `json:"personal_access_token"`
	}
	err = c.do(ctx, http.MethodPost, "/api/setup", map[string]any{
		"email":         email,
		"password":      password,
		"name":          name,
		"create_pat":    true,
		"pat_expire_in": 365,
	}, &out)
	if err != nil {
		return "", err
	}
	return out.PersonalAccessToken, nil
}

type SetupKey struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// CreateSetupKey issues a reusable enrollment token — pasted into the
// NetBird client app once, no browser login needed. name is just a label
// (we use the LDAP username) shown in NetBird's own records.
func (c *Client) CreateSetupKey(ctx context.Context, name string) (*SetupKey, error) {
	var out SetupKey
	err := c.do(ctx, http.MethodPost, "/api/setup-keys", map[string]any{
		"name":        name,
		"type":        "reusable",
		"expires_in":  2592000, // 30 days; re-enabling issues a fresh one
		"auto_groups": []string{},
		"usage_limit": 0,
	}, &out)
	if err != nil {
		return nil, fmt.Errorf("create setup key: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteSetupKey(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/setup-keys/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete setup key: %w", err)
	}
	return nil
}

type Peer struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Connected bool   `json:"connected"`
	LastSeen  string `json:"last_seen"`
	OS        string `json:"os"`
}

// ListPeers returns every device currently enrolled — NOT attributable to
// a specific company user: setup keys are reusable (one person's key can
// enroll several devices) and NetBird's peer objects don't reference which
// key created them, so this is deliberately a flat device list, not a
// per-user one.
func (c *Client) ListPeers(ctx context.Context) ([]Peer, error) {
	var out []Peer
	if err := c.do(ctx, http.MethodGet, "/api/peers", nil, &out); err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	return out, nil
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListGroups returns every peer group — used to find the ID of the "All"
// group NetBird creates automatically, needed as a route's distribution
// group (which peers get told about the route).
func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	var out []Group
	if err := c.do(ctx, http.MethodGet, "/api/groups", nil, &out); err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return out, nil
}

// CreateRoute advertises network (a CIDR, e.g. "10.201.28.2/32") as
// reachable via peerID to every peer in groupID — the mechanism behind
// both the LAN-gateway feature and private module routing: a device VPN
// clients get told about only because a specific peer said "I can reach
// this," not because it's on the public internet.
func (c *Client) CreateRoute(ctx context.Context, network, peerID, groupID string) error {
	return c.do(ctx, http.MethodPost, "/api/routes", map[string]any{
		"network":     network,
		"peer":        peerID,
		"description": "internal services gateway",
		"network_id":  "internal-services",
		"masquerade":  true,
		"enabled":     true,
		"metric":      100, // required, 1-9999 — no default, NetBird 422s without it
		"groups":      []string{groupID},
	}, nil)
}
