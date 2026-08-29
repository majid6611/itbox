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

// RegisterIdentityProvider registers an upstream OIDC connector (our Dex
// sidecar) with NetBird's embedded IdP.
func (c *Client) RegisterIdentityProvider(ctx context.Context, name, issuer, clientID, clientSecret string) error {
	return c.do(ctx, http.MethodPost, "/api/identity-providers", map[string]any{
		"type":          "oidc",
		"name":          name,
		"issuer":        issuer,
		"client_id":     clientID,
		"client_secret": clientSecret,
	}, nil)
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
