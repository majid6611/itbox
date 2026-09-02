// Package registryclient talks to registry-server (a separate service we
// run, see the repo's registry/ directory) to discover new/updated
// modules and download them — this platform's backend never talks to
// GitHub directly, only to registry-server's own thin API.
package registryclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type IndexEntry struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Category           string `json:"category"`
	LatestVersion      string `json:"latest_version"`
	Severity           string `json:"severity"` // "security" | "recommended" | "optional"
	Changelog          string `json:"changelog"`
	SHA256             string `json:"sha256"`
	MinPlatformVersion string `json:"min_platform_version,omitempty"`
}

type index struct {
	Modules []IndexEntry `json:"modules"`
}

type Client struct {
	BaseURL string
	Key     string

	HTTPClient *http.Client
}

func New(baseURL, key string) *Client {
	return &Client{BaseURL: baseURL, Key: key, HTTPClient: http.DefaultClient}
}

// Configured reports whether this deployment has a registry to talk to at
// all — most local/dev instances won't, and that's a normal, silent
// no-op state, not an error.
func (c *Client) Configured() bool {
	return c.BaseURL != "" && c.Key != ""
}

func (c *Client) authedRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	return req, nil
}

func (c *Client) FetchIndex(ctx context.Context) ([]IndexEntry, error) {
	req, err := c.authedRequest(ctx, http.MethodGet, c.BaseURL+"/v1/index")
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch index: unexpected status %d: %s", resp.StatusCode, body)
	}
	var parsed index
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return parsed.Modules, nil
}

// DownloadBundle streams a module version's tarball and verifies it
// against expectedSHA256 before returning the bytes — the registry index
// entry already carries this checksum, so a caller has it without a
// second request. Returning the full bytes (not a stream) is fine here:
// module bundles are a couple of small text files (manifest.yaml,
// docker-compose.yaml), nothing sized like a container image.
func (c *Client) DownloadBundle(ctx context.Context, id, version, expectedSHA256 string) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/modules/%s/%s/bundle", c.BaseURL, id, version)
	req, err := c.authedRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download bundle: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download bundle: unexpected status %d: %s", resp.StatusCode, body)
	}

	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if expectedSHA256 != "" && got != expectedSHA256 {
		return nil, fmt.Errorf("bundle checksum mismatch: expected %s, got %s", expectedSHA256, got)
	}
	return body, nil
}
