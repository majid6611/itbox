-- The system log: what the platform and its modules did (a module went down,
-- an update was rolled back, a backup failed, ...). Unlike the audit log this
-- is plain diagnostics — nobody is accountable for these rows, and they are
-- kept 90 days. Modules that share this database write rows here directly.
CREATE TABLE system_log (
    id BIGSERIAL PRIMARY KEY,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    level TEXT NOT NULL DEFAULT 'info',   -- info | warn | error
    source TEXT NOT NULL,                 -- "platform" or a module id
    message TEXT NOT NULL
);
CREATE INDEX system_log_at_idx ON system_log (at DESC);
CREATE INDEX system_log_source_idx ON system_log (source);
