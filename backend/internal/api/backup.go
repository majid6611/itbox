package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// webdavBackupNetwork is the docker network the throwaway rclone
// container needs to be on to resolve internal addresses — our own
// Garage bucket for a local destination, harmless (just unused) for a
// real AWS one.
const webdavBackupNetwork = "it-platform-edge"

type BackupConfig struct {
	Destination        string `json:"destination"` // "local" or "aws"
	AWSAccessKeyID      string `json:"aws_access_key_id"`
	AWSSecretAccessKey  string `json:"aws_secret_access_key"`
	AWSBucket           string `json:"aws_bucket"`
	AWSRegion           string `json:"aws_region"`
	Schedule            string `json:"schedule"` // "off", "daily", "weekly"
}

func (s *Server) getBackupConfig(ctx context.Context) (BackupConfig, error) {
	var c BackupConfig
	err := s.DB.QueryRow(ctx, `
		SELECT destination, aws_access_key_id, aws_secret_access_key, aws_bucket, aws_region, schedule
		FROM backup_config WHERE id = true
	`).Scan(&c.Destination, &c.AWSAccessKeyID, &c.AWSSecretAccessKey, &c.AWSBucket, &c.AWSRegion, &c.Schedule)
	if err != nil {
		// No row yet — defaults match the migration's column defaults.
		return BackupConfig{Destination: "local", Schedule: "off"}, nil
	}
	return c, nil
}

func (s *Server) saveBackupConfig(ctx context.Context, c BackupConfig) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO backup_config (id, destination, aws_access_key_id, aws_secret_access_key, aws_bucket, aws_region, schedule)
		VALUES (true, $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			destination = $1, aws_access_key_id = $2, aws_secret_access_key = $3,
			aws_bucket = $4, aws_region = $5, schedule = $6
	`, c.Destination, c.AWSAccessKeyID, c.AWSSecretAccessKey, c.AWSBucket, c.AWSRegion, c.Schedule)
	return err
}

// rcloneRemote resolves the currently-configured backup destination into
// the env vars rclone needs (no config file — everything via
// RCLONE_CONFIG_BACKUP_*) and the "backup:<bucket>/webdav" remote path
// both backup and restore address. Shared by both directions since the
// destination is the same either way — only which side of `rclone copy`
// it lands on differs.
func (s *Server) rcloneRemote(ctx context.Context) (env map[string]string, remotePath string, err error) {
	cfg, err := s.getBackupConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("load backup config: %w", err)
	}

	switch cfg.Destination {
	case "aws":
		if cfg.AWSAccessKeyID == "" || cfg.AWSSecretAccessKey == "" || cfg.AWSBucket == "" {
			return nil, "", fmt.Errorf("AWS backup isn't fully configured yet — set access key, secret key, and bucket in Settings")
		}
		env = map[string]string{
			"RCLONE_CONFIG_BACKUP_TYPE":              "s3",
			"RCLONE_CONFIG_BACKUP_PROVIDER":          "AWS",
			"RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID":     cfg.AWSAccessKeyID,
			"RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY": cfg.AWSSecretAccessKey,
			"RCLONE_CONFIG_BACKUP_REGION":            cfg.AWSRegion,
		}
		return env, "backup:" + cfg.AWSBucket + "/webdav", nil
	default: // "local"
		s3Status, ok, err := s.Modules.GetInstalled(ctx, "s3-storage")
		if err != nil || !ok || s3Status.Status != "running" {
			return nil, "", fmt.Errorf("install the Backup Storage module first")
		}
		endpoint := "http://" + s.Modules.ServiceAddr("s3-storage", "garage", 3900)
		env = map[string]string{
			"RCLONE_CONFIG_BACKUP_TYPE":              "s3",
			"RCLONE_CONFIG_BACKUP_PROVIDER":          "Other",
			"RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID":     s3Status.Config["access_key"],
			"RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY": s3Status.Config["secret_key"],
			"RCLONE_CONFIG_BACKUP_ENDPOINT":          endpoint,
			"RCLONE_CONFIG_BACKUP_REGION":            "garage",
		}
		return env, "backup:" + s3Status.Config["default_bucket"] + "/webdav", nil
	}
}

func (s *Server) startRun(ctx context.Context, kind string) (finish func(status string, cause error), ok bool) {
	var runID int
	err := s.DB.QueryRow(ctx, `INSERT INTO backup_runs (kind, status) VALUES ($1, 'running') RETURNING id`, kind).Scan(&runID)
	if err != nil {
		log.Printf("%s: record run start: %v", kind, err)
		return nil, false
	}
	return func(status string, cause error) {
		msg := ""
		if cause != nil {
			msg = cause.Error()
			if len(msg) > 2000 {
				msg = msg[:2000] // rclone -v output can be long; keep it readable
			}
		}
		if _, err := s.DB.Exec(ctx, `UPDATE backup_runs SET finished_at = now(), status = $2, error_message = $3 WHERE id = $1`, runID, status, msg); err != nil {
			log.Printf("%s: record run finish: %v", kind, err)
		}
	}, true
}

// runBackup does one WebDAV -> S3 copy (never sync — see
// docker.Client.RunRcloneCopy for why), recording the attempt in
// backup_runs regardless of outcome so the panel can show real history,
// not just "it's running."
func (s *Server) runBackup(ctx context.Context) {
	finish, ok := s.startRun(ctx, "backup")
	if !ok {
		return
	}

	webdavStatus, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil || !ok || webdavStatus.Status != "running" {
		finish("error", fmt.Errorf("install the fileshare (WebDAV) module first — there's nothing to back up"))
		return
	}
	webdavVolume := s.Modules.VolumeName("fileshare-webdav", "webdav_data")

	env, remotePath, err := s.rcloneRemote(ctx)
	if err != nil {
		finish("error", err)
		return
	}

	if _, err := s.Docker.RunRcloneCopy(ctx, webdavVolume, false, webdavBackupNetwork, env, "/data", remotePath); err != nil {
		finish("error", err)
		return
	}
	finish("success", nil)
}

// runRestore copies the other direction, S3 -> WebDAV, and is
// deliberately additive: `rclone copy` only adds/updates files at the
// destination, so restoring can never delete something already in
// WebDAV that isn't in the backup (e.g. a file created since the last
// backup ran) — safe to click without a "this will wipe X" warning.
func (s *Server) runRestore(ctx context.Context) {
	finish, ok := s.startRun(ctx, "restore")
	if !ok {
		return
	}

	webdavStatus, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil || !ok || webdavStatus.Status != "running" {
		finish("error", fmt.Errorf("install the fileshare (WebDAV) module first"))
		return
	}
	webdavVolume := s.Modules.VolumeName("fileshare-webdav", "webdav_data")

	env, remotePath, err := s.rcloneRemote(ctx)
	if err != nil {
		finish("error", err)
		return
	}

	if _, err := s.Docker.RunRcloneCopy(ctx, webdavVolume, true, webdavBackupNetwork, env, remotePath, "/data"); err != nil {
		finish("error", err)
		return
	}
	finish("success", nil)
}

// startBackupScheduler checks once an hour whether a scheduled backup is
// due — coarse but simple, matching the daily/weekly granularity a small
// company actually needs. Not a real cron daemon: no extra dependency for
// something this infrequent.
func (s *Server) startBackupScheduler(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			s.maybeRunScheduledBackup(ctx)
		}
	}()
}

func (s *Server) maybeRunScheduledBackup(ctx context.Context) {
	cfg, err := s.getBackupConfig(ctx)
	if err != nil || cfg.Schedule == "off" {
		return
	}
	interval := 24 * time.Hour
	if cfg.Schedule == "weekly" {
		interval = 7 * 24 * time.Hour
	}

	var lastRun time.Time
	err = s.DB.QueryRow(ctx, `SELECT started_at FROM backup_runs WHERE kind = 'backup' ORDER BY started_at DESC LIMIT 1`).Scan(&lastRun)
	if err == nil && time.Since(lastRun) < interval {
		return
	}
	go s.runBackup(context.Background())
}

type GetBackupConfigInput struct {
	SessionToken string `cookie:"itp_session"`
}

type GetBackupConfigOutput struct {
	Body BackupConfig
}

type SetBackupConfigInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         BackupConfig
}

type BackupRunOut struct {
	Kind         string  `json:"kind"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   *string `json:"finished_at,omitempty"`
	Status       string  `json:"status"`
	ErrorMessage *string `json:"error_message,omitempty"`
}

type ListBackupRunsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type ListBackupRunsOutput struct {
	Body struct {
		Runs []BackupRunOut `json:"runs"`
	}
}

func registerBackup(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-backup-config",
		Method:      "GET",
		Path:        "/api/backup/config",
		Summary:     "Get the WebDAV backup destination and schedule",
	}, func(ctx context.Context, in *GetBackupConfigInput) (*GetBackupConfigOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		cfg, err := s.getBackupConfig(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("load backup config", err)
		}
		cfg.AWSSecretAccessKey = "" // never echo the secret back
		out := &GetBackupConfigOutput{Body: cfg}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-backup-config",
		Method:      "POST",
		Path:        "/api/backup/config",
		Summary:     "Set the WebDAV backup destination and schedule",
	}, func(ctx context.Context, in *SetBackupConfigInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if in.Body.Destination != "local" && in.Body.Destination != "aws" {
			return nil, huma.Error400BadRequest(`destination must be "local" or "aws"`)
		}
		if in.Body.Schedule != "off" && in.Body.Schedule != "daily" && in.Body.Schedule != "weekly" {
			return nil, huma.Error400BadRequest(`schedule must be "off", "daily", or "weekly"`)
		}
		// A blank secret in the request means "keep the existing one" —
		// the get endpoint never echoes it back, so the form can't
		// round-trip it otherwise.
		if in.Body.Destination == "aws" && in.Body.AWSSecretAccessKey == "" {
			existing, err := s.getBackupConfig(ctx)
			if err == nil {
				in.Body.AWSSecretAccessKey = existing.AWSSecretAccessKey
			}
		}
		if err := s.saveBackupConfig(ctx, in.Body); err != nil {
			return nil, huma.Error500InternalServerError("save backup config", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "run-backup-now",
		Method:      "POST",
		Path:        "/api/backup/run",
		Summary:     "Start a WebDAV backup in the background; poll GET /api/backup/history for status",
	}, func(ctx context.Context, in *GetBackupConfigInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		go s.runBackup(context.Background())
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "run-restore-now",
		Method:      "POST",
		Path:        "/api/backup/restore",
		Summary:     "Restore files from the backup into WebDAV — additive, never deletes anything already there",
	}, func(ctx context.Context, in *GetBackupConfigInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		go s.runRestore(context.Background())
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-backup-runs",
		Method:      "GET",
		Path:        "/api/backup/history",
		Summary:     "List recent backup and restore attempts",
	}, func(ctx context.Context, in *ListBackupRunsInput) (*ListBackupRunsOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		rows, err := s.DB.Query(ctx, `
			SELECT kind, started_at, finished_at, status, error_message
			FROM backup_runs ORDER BY started_at DESC LIMIT 20
		`)
		if err != nil {
			return nil, huma.Error500InternalServerError("list backup runs", err)
		}
		defer rows.Close()
		out := &ListBackupRunsOutput{}
		for rows.Next() {
			var r BackupRunOut
			var startedAt time.Time
			var finishedAt *time.Time
			if err := rows.Scan(&r.Kind, &startedAt, &finishedAt, &r.Status, &r.ErrorMessage); err != nil {
				return nil, huma.Error500InternalServerError("scan backup run", err)
			}
			r.StartedAt = startedAt.Format(time.RFC3339)
			if finishedAt != nil {
				f := finishedAt.Format(time.RFC3339)
				r.FinishedAt = &f
			}
			out.Body.Runs = append(out.Body.Runs, r)
		}
		return out, nil
	})
}
