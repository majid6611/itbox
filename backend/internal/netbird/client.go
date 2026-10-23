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
	"strings"
	"time"
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

// SetupKeyLifetime is how long an enrollment key works. It enrolls exactly
// one device, so a key that leaks or sits unused in someone's inbox is
// worthless after a day.
const SetupKeyLifetime = 24 * time.Hour

// CreateSetupKey issues a one-device, one-day enrollment token — pasted into
// the NetBird client app once, no browser login needed. name is just a label
// (we use the LDAP username) shown in NetBird's own records.
//
// groupIDs are the groups every device enrolled with this key joins, which
// is the only way to tell afterwards whose device is whose (see Peer).
func (c *Client) CreateSetupKey(ctx context.Context, name string, groupIDs []string) (*SetupKey, error) {
	return c.createSetupKey(ctx, name, groupIDs, "one-off", SetupKeyLifetime, 1)
}

// CreateReusableSetupKey is for the platform's own gateway, whose start-up
// retries `netbird up` with the same key until it works — a one-device key
// would be used up by an attempt that registered but then timed out.
func (c *Client) CreateReusableSetupKey(ctx context.Context, name string, groupIDs []string) (*SetupKey, error) {
	return c.createSetupKey(ctx, name, groupIDs, "reusable", 30*24*time.Hour, 0)
}

func (c *Client) createSetupKey(ctx context.Context, name string, groupIDs []string, kind string, ttl time.Duration, usageLimit int) (*SetupKey, error) {
	var out SetupKey
	err := c.do(ctx, http.MethodPost, "/api/setup-keys", map[string]any{
		"name":        name,
		"type":        kind,
		"expires_in":  int(ttl.Seconds()),
		"auto_groups": groupIDs,
		"usage_limit": usageLimit,
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
	// Groups is how a device is tied back to a user: enrolling with that
	// user's setup key puts it in their personal group.
	Groups []Group `json:"groups"`
}

func (c *Client) DeletePeer(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/peers/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete peer: %w", err)
	}
	return nil
}

func (c *Client) CreateGroup(ctx context.Context, name string) (*Group, error) {
	var out Group
	if err := c.do(ctx, http.MethodPost, "/api/groups", map[string]any{"name": name}, &out); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/groups/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

// ListPeers returns every device currently enrolled. Which user a device
// belongs to is its "user-<name>" group (see Peer.Groups).
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
	// Peers is only filled in by GetGroup.
	Peers []struct {
		ID string `json:"id"`
	} `json:"peers"`
}

// AddPeerToGroup puts a peer in a group (a no-op if it's already there).
// NetBird only offers "replace the whole member list," so this reads the
// current one first.
func (c *Client) AddPeerToGroup(ctx context.Context, groupID, peerID string) error {
	var g Group
	if err := c.do(ctx, http.MethodGet, "/api/groups/"+groupID, nil, &g); err != nil {
		return fmt.Errorf("get group: %w", err)
	}
	ids := []string{}
	for _, p := range g.Peers {
		if p.ID == peerID {
			return nil
		}
		ids = append(ids, p.ID)
	}
	return c.setGroupPeers(ctx, g, append(ids, peerID))
}

func (c *Client) setGroupPeers(ctx context.Context, g Group, peerIDs []string) error {
	err := c.do(ctx, http.MethodPut, "/api/groups/"+g.ID, map[string]any{"name": g.Name, "peers": peerIDs}, nil)
	if err != nil {
		return fmt.Errorf("update group: %w", err)
	}
	return nil
}

// AddAllPeersToGroup puts every currently enrolled peer in the group.
func (c *Client) AddAllPeersToGroup(ctx context.Context, g Group) error {
	peers, err := c.ListPeers(ctx)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, p := range peers {
		ids = append(ids, p.ID)
	}
	return c.setGroupPeers(ctx, g, ids)
}

type Policy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListPolicies(ctx context.Context) ([]Policy, error) {
	var out []Policy
	if err := c.do(ctx, http.MethodGet, "/api/policies", nil, &out); err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	return out, nil
}

func (c *Client) DeletePolicy(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/policies/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

// CreateAllowPolicy lets every device in the group reach every other device
// in it, on any port. Devices outside the group are reachable by nobody.
func (c *Client) CreateAllowPolicy(ctx context.Context, name, groupID string) error {
	err := c.do(ctx, http.MethodPost, "/api/policies", map[string]any{
		"name":    name,
		"enabled": true,
		"rules": []map[string]any{{
			"name":          name,
			"enabled":       true,
			"action":        "accept",
			"bidirectional": true,
			"protocol":      "all",
			"sources":       []string{groupID},
			"destinations":  []string{groupID},
		}},
	}, nil)
	if err != nil {
		return fmt.Errorf("create policy: %w", err)
	}
	return nil
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

type Route struct {
	ID        string `json:"id"`
	NetworkID string `json:"network_id"`
	Network   string `json:"network"`
	Peer      string `json:"peer"`
	// Groups are the peers told about this route.
	Groups []string `json:"groups"`
}

func (c *Client) ListRoutes(ctx context.Context) ([]Route, error) {
	var out []Route
	if err := c.do(ctx, http.MethodGet, "/api/routes", nil, &out); err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	return out, nil
}

func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/routes/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

// CreateRoute advertises network (a CIDR, e.g. "10.201.28.2/32") as
// reachable via peerID to every peer in groupID — the mechanism behind
// both private module routing and office-network access: a device VPN
// clients get told about only because a specific peer said "I can reach
// this," not because it's on the public internet. networkID is the
// route's stable name, what syncGatewayRoutes matches existing routes by.
func (c *Client) CreateRoute(ctx context.Context, networkID, network, description, peerID, groupID string) error {
	return c.do(ctx, http.MethodPost, "/api/routes", map[string]any{
		"network":     network,
		"peer":        peerID,
		"description": description,
		"network_id":  networkID,
		"masquerade":  true,
		"enabled":     true,
		"metric":      100, // required, 1-9999 — no default, NetBird 422s without it
		"groups":      []string{groupID},
	}, nil)
}

// SetLocalMFA turns the authenticator-app step on or off for every person
// who logs in with a NetBird login. NetBird replaces all account settings
// in one PUT, so the current ones are read first and sent back unchanged.
func (c *Client) SetLocalMFA(ctx context.Context, enabled bool) error {
	var accounts []struct {
		ID       string         `json:"id"`
		Settings map[string]any `json:"settings"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/accounts", nil, &accounts); err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	if len(accounts) == 0 {
		return fmt.Errorf("get account: none found")
	}
	a := accounts[0]
	if on, _ := a.Settings["local_mfa_enabled"].(bool); on == enabled {
		return nil
	}
	a.Settings["local_mfa_enabled"] = enabled
	if err := c.do(ctx, http.MethodPut, "/api/accounts/"+a.ID, map[string]any{"settings": a.Settings}, nil); err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

type User struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

// CreateLocalUser makes a person who can log in to the VPN app with an email
// and a generated password (returned only now). Every device they log in
// with joins autoGroups.
func (c *Client) CreateLocalUser(ctx context.Context, email, name string, autoGroups []string) (*User, error) {
	var out User
	err := c.do(ctx, http.MethodPost, "/api/users", map[string]any{
		"email":           email,
		"name":            name,
		"role":            "user",
		"auto_groups":     autoGroups,
		"is_service_user": false,
	}, &out)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteUser(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/users/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// A NetBird "network" is a set of addresses reachable through a router peer,
// with access to them controlled by ordinary policies — which is what makes
// per-port rules possible (the older route kind lets everything through).
type Network struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Router struct {
	ID   string `json:"id"`
	Peer string `json:"peer"`
}

type Resource struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var out []Network
	if err := c.do(ctx, http.MethodGet, "/api/networks", nil, &out); err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	return out, nil
}

func (c *Client) CreateNetwork(ctx context.Context, name string) (*Network, error) {
	var out Network
	if err := c.do(ctx, http.MethodPost, "/api/networks", map[string]any{"name": name}, &out); err != nil {
		return nil, fmt.Errorf("create network: %w", err)
	}
	return &out, nil
}

// DeleteNetwork also removes the network's routers and resources.
func (c *Client) DeleteNetwork(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/networks/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete network: %w", err)
	}
	return nil
}

func (c *Client) ListRouters(ctx context.Context, networkID string) ([]Router, error) {
	var out []Router
	if err := c.do(ctx, http.MethodGet, "/api/networks/"+networkID+"/routers", nil, &out); err != nil {
		return nil, fmt.Errorf("list routers: %w", err)
	}
	return out, nil
}

func (c *Client) CreateRouter(ctx context.Context, networkID, peerID string) error {
	err := c.do(ctx, http.MethodPost, "/api/networks/"+networkID+"/routers", map[string]any{
		"peer":       peerID,
		"metric":     100,
		"masquerade": true,
		"enabled":    true,
	}, nil)
	if err != nil {
		return fmt.Errorf("create router: %w", err)
	}
	return nil
}

func (c *Client) DeleteRouter(ctx context.Context, networkID, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/networks/"+networkID+"/routers/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete router: %w", err)
	}
	return nil
}

func (c *Client) ListResources(ctx context.Context, networkID string) ([]Resource, error) {
	var out []Resource
	if err := c.do(ctx, http.MethodGet, "/api/networks/"+networkID+"/resources", nil, &out); err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	return out, nil
}

// CreateResource adds an address range to a network; a policy that names one
// of groupIDs as its destination is what lets devices reach it.
func (c *Client) CreateResource(ctx context.Context, networkID, name, address string, groupIDs []string) error {
	err := c.do(ctx, http.MethodPost, "/api/networks/"+networkID+"/resources", map[string]any{
		"name":    name,
		"address": address,
		"enabled": true,
		"groups":  groupIDs,
	}, nil)
	if err != nil {
		return fmt.Errorf("create resource: %w", err)
	}
	return nil
}

func (c *Client) DeleteResource(ctx context.Context, networkID, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/networks/"+networkID+"/resources/"+id, nil, nil); err != nil {
		return fmt.Errorf("delete resource: %w", err)
	}
	return nil
}

// PortRange is an inclusive range of ports.
type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// PolicyRule lets devices in Source reach Destination over one protocol
// ("all", "tcp", "udp" or "icmp"), on the listed ports if any (every port
// when both lists are empty).
type PolicyRule struct {
	Protocol   string
	Ports      []string
	PortRanges []PortRange
}

// SetRulePolicies makes the policies "<prefix> 1", "<prefix> 2", ... match
// rules: one policy per rule, each from the source group to the destination
// group. One policy per rule, not one policy with several rules, because
// NetBird only applies the first rule of a policy to traffic going to a
// network (found by testing: a TCP rule followed by an ICMP rule silently
// blocked the ping). Policies left over from a longer earlier list go.
func (c *Client) SetRulePolicies(ctx context.Context, prefix, sourceID, destinationID string, rules []PolicyRule) error {
	policies, err := c.ListPolicies(ctx)
	if err != nil {
		return err
	}
	existing := map[string]string{} // name -> id
	for _, p := range policies {
		existing[p.Name] = p.ID
	}
	wanted := map[string]bool{}
	for i, r := range rules {
		name := fmt.Sprintf("%s %d", prefix, i+1)
		wanted[name] = true
		rule := map[string]any{
			"name":          name,
			"enabled":       true,
			"action":        "accept",
			"bidirectional": false,
			"protocol":      r.Protocol,
			"sources":       []string{sourceID},
			"destinations":  []string{destinationID},
		}
		if len(r.Ports) > 0 {
			rule["ports"] = r.Ports
		}
		if len(r.PortRanges) > 0 {
			rule["port_ranges"] = r.PortRanges
		}
		body := map[string]any{"name": name, "enabled": true, "rules": []map[string]any{rule}}
		if id, ok := existing[name]; ok {
			if err := c.do(ctx, http.MethodPut, "/api/policies/"+id, body, nil); err != nil {
				return fmt.Errorf("update policy: %w", err)
			}
		} else if err := c.do(ctx, http.MethodPost, "/api/policies", body, nil); err != nil {
			return fmt.Errorf("create policy: %w", err)
		}
	}
	return c.DeletePoliciesWithPrefix(ctx, prefix, wanted)
}

// DeletePoliciesWithPrefix removes every policy named "<prefix> <something>"
// except those in keep.
func (c *Client) DeletePoliciesWithPrefix(ctx context.Context, prefix string, keep map[string]bool) error {
	policies, err := c.ListPolicies(ctx)
	if err != nil {
		return err
	}
	for _, p := range policies {
		if strings.HasPrefix(p.Name, prefix+" ") && !keep[p.Name] {
			if err := c.DeletePolicy(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
