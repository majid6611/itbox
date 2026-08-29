-- Single-row backup destination config, same singleton pattern as
-- platform_settings. "local" (the default) uses the s3-storage module's
-- own auto-created bucket — nothing to configure. "aws" needs real AWS
-- credentials, entered here.
CREATE TABLE backup_config (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    destination TEXT NOT NULL DEFAULT 'local',
    aws_access_key_id TEXT NOT NULL DEFAULT '',
    aws_secret_access_key TEXT NOT NULL DEFAULT '',
    aws_bucket TEXT NOT NULL DEFAULT '',
    aws_region TEXT NOT NULL DEFAULT '',
    -- 'off' | 'daily' | 'weekly' — checked by an in-process ticker, not a
    -- real cron daemon; coarse granularity is all a small company backup
    -- needs, so a periodic check is simpler than adding a cron library.
    schedule TEXT NOT NULL DEFAULT 'off'
);

-- One row per backup attempt — lets the panel show "last backup: success,
-- 2 hours ago" without digging into container logs.
CREATE TABLE backup_runs (
    id SERIAL PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'running', -- running | success | error
    error_message TEXT
);
