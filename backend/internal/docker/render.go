package docker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RenderCompose copies a module's docker-compose.yaml from its catalog
// directory into its per-install data directory, and writes a .env file
// from the resolved config so the compose file's ${VAR} substitutions
// pick it up. Returns the data directory the stack was rendered into.
func RenderCompose(catalogDir, composeFile, dataDir string, env map[string]string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}

	src, err := os.Open(filepath.Join(catalogDir, composeFile))
	if err != nil {
		return "", fmt.Errorf("open compose file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(filepath.Join(dataDir, "docker-compose.yaml"))
	if err != nil {
		return "", fmt.Errorf("create compose file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copy compose file: %w", err)
	}

	envFile, err := os.Create(filepath.Join(dataDir, ".env"))
	if err != nil {
		return "", fmt.Errorf("create env file: %w", err)
	}
	defer envFile.Close()

	for k, v := range env {
		if _, err := fmt.Fprintf(envFile, "%s=%s\n", k, v); err != nil {
			return "", fmt.Errorf("write env file: %w", err)
		}
	}

	return dataDir, nil
}
