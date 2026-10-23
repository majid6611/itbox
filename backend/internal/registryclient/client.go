// Package registryclient finds new and updated modules by reading the
// platform's own public GitHub repository (the modules/ folder on its main
// branch) — there is no separate update server to run or keys to hand out.
// The backend downloads the repo's tarball, reads each module's
// manifest.yaml for its version, and repacks a module's folder as the
// bundle that gets installed over the old one.
package registryclient

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type IndexEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Category      string `json:"category"`
	LatestVersion string `json:"latest_version"`
	Severity      string `json:"severity"` // "security" | "recommended" | "optional"
	Changelog     string `json:"changelog"`
	SHA256        string `json:"sha256"`
}

type Client struct {
	// Repo is "owner/name" on GitHub; Ref the branch to read.
	Repo string
	Ref  string
	// BaseURL is where the tarball is fetched from; overridable for tests.
	BaseURL string

	HTTPClient *http.Client

	mu      sync.Mutex
	bundles map[string]bundle // by module id, from the last FetchIndex
}

type bundle struct {
	version string
	data    []byte
}

func New(repo string) *Client {
	return &Client{
		Repo:       repo,
		Ref:        "main",
		BaseURL:    "https://codeload.github.com",
		HTTPClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Configured reports whether there is a repository to read at all.
func (c *Client) Configured() bool {
	return c != nil && c.Repo != ""
}

// maxTarball caps what we read from the network: the whole repo is a few
// megabytes, so anything far beyond that is wrong.
const maxTarball = 200 << 20

type manifestHead struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Category string `yaml:"category"`
	Version  string `yaml:"version"`
}

// FetchIndex downloads the repository once and lists every module in it,
// keeping each module's repacked bundle for DownloadBundle.
func (c *Client) FetchIndex(ctx context.Context) ([]IndexEntry, error) {
	url := fmt.Sprintf("%s/%s/tar.gz/refs/heads/%s", c.BaseURL, c.Repo, c.Ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", c.Repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %d", c.Repo, resp.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxTarball))
	if err != nil {
		return nil, fmt.Errorf("open repository archive: %w", err)
	}
	defer gz.Close()

	// Files per module, keyed by path inside the module's own folder.
	type file struct {
		mode int64
		data []byte
	}
	files := map[string]map[string]file{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read repository archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// GitHub puts everything under one "<repo>-<ref>/" folder.
		_, rest, ok := strings.Cut(hdr.Name, "/")
		if !ok {
			continue
		}
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) != 3 || parts[0] != "modules" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		if files[parts[1]] == nil {
			files[parts[1]] = map[string]file{}
		}
		files[parts[1]][parts[2]] = file{mode: hdr.Mode, data: data}
	}

	var entries []IndexEntry
	bundles := map[string]bundle{}
	for dir, fs := range files {
		mf, ok := fs["manifest.yaml"]
		if !ok {
			continue
		}
		var head manifestHead
		if err := yaml.Unmarshal(mf.data, &head); err != nil || head.ID == "" || head.ID != dir || head.Version == "" {
			continue // not a usable module; skip rather than fail the whole check
		}

		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		for name, f := range fs {
			if err := tw.WriteHeader(&tar.Header{Name: path.Clean(name), Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
				return nil, err
			}
			if _, err := tw.Write(f.data); err != nil {
				return nil, err
			}
		}
		if err := tw.Close(); err != nil {
			return nil, err
		}
		if err := gw.Close(); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(buf.Bytes())
		bundles[head.ID] = bundle{version: head.Version, data: buf.Bytes()}
		entries = append(entries, IndexEntry{
			ID:            head.ID,
			Name:          head.Name,
			Category:      head.Category,
			LatestVersion: head.Version,
			Severity:      "recommended",
			SHA256:        hex.EncodeToString(sum[:]),
		})
	}

	c.mu.Lock()
	c.bundles = bundles
	c.mu.Unlock()
	return entries, nil
}

// DownloadBundle returns the bundle for a module version found by the last
// FetchIndex, re-checking its checksum.
func (c *Client) DownloadBundle(ctx context.Context, id, version, expectedSHA256 string) ([]byte, error) {
	c.mu.Lock()
	b, ok := c.bundles[id]
	c.mu.Unlock()
	if !ok || b.version != version {
		// Not held (e.g. the backend restarted since the check): look again.
		if _, err := c.FetchIndex(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		b, ok = c.bundles[id]
		c.mu.Unlock()
		if !ok || b.version != version {
			return nil, fmt.Errorf("module %s version %s is no longer in the repository", id, version)
		}
	}
	sum := sha256.Sum256(b.data)
	if expectedSHA256 != "" && hex.EncodeToString(sum[:]) != expectedSHA256 {
		return nil, fmt.Errorf("bundle checksum mismatch for %s", id)
	}
	return b.data, nil
}
