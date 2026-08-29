// Package webdavfs creates per-user folders in the WebDAV module. WebDAV's
// MKCOL (make collection) is just one HTTP method with basic auth — no
// client library needed for something this small.
package webdavfs

import (
	"context"
	"fmt"
	"net/http"
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{baseURL: baseURL, username: username, password: password, http: &http.Client{}}
}

// EnsureFolder creates a top-level folder named after the given path
// segment (e.g. a username), if it doesn't already exist.
func (c *Client) EnsureFolder(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, "MKCOL", c.baseURL+"/"+name+"/", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mkcol: %w", err)
	}
	defer resp.Body.Close()

	// 201 Created: made it. 405 Method Not Allowed: WebDAV's way of saying
	// the collection already exists — also fine, nothing to do.
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil
	}
	return fmt.Errorf("mkcol %s: unexpected status %d", name, resp.StatusCode)
}
