-- Restore shares the same run-tracking table as backups (same shape,
-- same history UI) — this just tells them apart.
ALTER TABLE backup_runs ADD COLUMN kind TEXT NOT NULL DEFAULT 'backup';
