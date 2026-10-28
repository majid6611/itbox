-- One small row every few minutes so the dashboard can show how disk, memory
-- and load have been moving. Kept 30 days, then pruned.
CREATE TABLE health_samples (
    ts       TIMESTAMPTZ PRIMARY KEY,
    disk_pct REAL NOT NULL,
    mem_pct  REAL NOT NULL,
    load1    REAL NOT NULL,
    bad      INT  NOT NULL,
    warn     INT  NOT NULL
);
