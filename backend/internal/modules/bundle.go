package modules

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractBundle unpacks a module bundle (manifest.yaml, docker-compose.yaml,
// and whatever else that module's manifest.Dir normally holds — the exact
// tree the publish script tars up from modules/<id>/) into destDir,
// overwriting whatever's there. destDir is created first so a brand-new
// module (no prior directory at all) works the same as updating an
// existing one.
func extractBundle(data []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create module dir: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// "tar -C <dir> ." (the publish script's own invocation) always
		// emits a top-level "./" directory entry for <dir> itself — not
		// an escape attempt, just skip it.
		cleaned := filepath.Clean(hdr.Name)
		if cleaned == "." {
			continue
		}
		// Guard against a path actually escaping destDir (a malicious or
		// corrupt archive using "../" segments) — this bundle came over
		// the network and got its bytes checksum-verified, but not its
		// internal structure, so this check stays regardless.
		if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
			return fmt.Errorf("bundle entry %q escapes the module directory", hdr.Name)
		}
		target := filepath.Join(destDir, cleaned)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()
		}
	}
}
