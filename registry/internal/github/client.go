// Package github wraps just the two GitHub REST calls registry-server
// needs — fetching index.json's contents and streaming a release asset —
// against a private repo, using our own server-side token. Nothing here
// is ever handed to a client server; they only ever talk to
// registry-server's own /v1 endpoints, never to GitHub.
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	// BaseURL defaults to https://api.github.com — overridable so tests
	// (and local dev, before there's a real published index/release) can
	// point this at a stand-in HTTP server with the same two response
	// shapes instead of live GitHub.
	BaseURL string
	Token   string
	Owner   string
	Repo    string

	HTTPClient *http.Client
}

func New(baseURL, token, owner, repo string) *Client {
	return &Client{BaseURL: baseURL, Token: token, Owner: owner, Repo: repo, HTTPClient: http.DefaultClient}
}

func (c *Client) authedRequest(method, url, accept string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

// contentsResponse is the shape of GitHub's "get repository content" API
// for a single file — base64-encoded, not the raw bytes, unless the
// caller asks for the raw media type instead (which redirects through a
// signed, unauthenticated URL that a private repo's real content isn't
// served at — so this fetches the JSON form and decodes it, rather than
// relying on the raw media type working for private repos).
type contentsResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// FetchIndex returns index.json's current contents from the repo's
// default branch.
func (c *Client) FetchIndex(ctx context.Context) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/index.json", c.BaseURL, c.Owner, c.Repo)
	req, err := c.authedRequest(http.MethodGet, url, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read index response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch index: unexpected status %d: %s", resp.StatusCode, body)
	}

	var parsed contentsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse index response: %w", err)
	}
	if parsed.Encoding != "base64" {
		return nil, fmt.Errorf("fetch index: unexpected encoding %q", parsed.Encoding)
	}
	// GitHub's base64 content is wrapped with newlines every 60 chars —
	// the stdlib decoder rejects those, so strip whitespace first.
	decoded, err := base64.StdEncoding.DecodeString(stripWhitespace(parsed.Content))
	if err != nil {
		return nil, fmt.Errorf("decode index content: %w", err)
	}
	return decoded, nil
}

func stripWhitespace(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' && s[i] != '\r' && s[i] != ' ' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

type release struct {
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"url"` // API asset URL, not browser_download_url — required to auth-fetch a private repo's asset
	} `json:"assets"`
}

// FetchBundleAsset streams a release's named asset back. tag identifies
// the release (registry-server's own convention: "<module id>-v<version>"
// — see the publish script), assetName the file within it
// ("<module id>-<version>.tar.gz"). The caller must Close the returned
// reader.
func (c *Client) FetchBundleAsset(ctx context.Context, tag, assetName string) (io.ReadCloser, error) {
	relURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.BaseURL, c.Owner, c.Repo, tag)
	req, err := c.authedRequest(http.MethodGet, relURL, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release %s: %w", tag, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read release response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch release %s: unexpected status %d: %s", tag, resp.StatusCode, body)
	}

	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("parse release response: %w", err)
	}
	var assetURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("release %s has no asset named %q", tag, assetName)
	}

	assetReq, err := c.authedRequest(http.MethodGet, assetURL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	assetReq = assetReq.WithContext(ctx)
	assetResp, err := c.HTTPClient.Do(assetReq)
	if err != nil {
		return nil, fmt.Errorf("fetch asset %s: %w", assetName, err)
	}
	if assetResp.StatusCode != http.StatusOK {
		defer assetResp.Body.Close()
		errBody, _ := io.ReadAll(assetResp.Body)
		return nil, fmt.Errorf("fetch asset %s: unexpected status %d: %s", assetName, assetResp.StatusCode, errBody)
	}
	return assetResp.Body, nil
}
