package s3client

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FromInstalledModules builds a Client for the s3-storage module by
// reading its connection details directly from installed_modules — same
// shared-DB pattern as the directory package's LDAP lookup. Returns
// available=false if the module isn't installed and running.
func FromInstalledModules(ctx context.Context, db *pgxpool.Pool) (client *Client, available bool, err error) {
	var status string
	var configJSON []byte
	err = db.QueryRow(ctx, `SELECT status, config FROM installed_modules WHERE module_id = 's3-storage'`).Scan(&status, &configJSON)
	if err != nil {
		return nil, false, nil // not installed
	}
	if status != "running" {
		return nil, false, nil
	}
	var cfg struct {
		DefaultBucket string `json:"default_bucket"`
		AccessKey     string `json:"access_key"`
		SecretKey     string `json:"secret_key"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, false, err
	}
	// Fixed internal hostname:port, same convention as directory.go — the
	// s3-storage module's own compose file names its service "garage" on
	// port 3900.
	endpoint := "http://itp-s3-storage-garage-1:3900"
	return New(endpoint, cfg.AccessKey, cfg.SecretKey, cfg.DefaultBucket), true, nil
}
