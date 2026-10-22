-- Uninstalling a module keeps its data volumes (that is deliberate: no data
-- is deleted), but those volumes were initialised with the module's
-- generated secrets. Keeping the last config here lets a later reinstall
-- reuse the same secrets instead of generating new ones that no longer match
-- what is stored in the volumes.
CREATE TABLE module_config_archive (
    module_id TEXT PRIMARY KEY,
    config JSONB NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
