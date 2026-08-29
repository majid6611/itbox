-- Single-row table (id always true) for platform-wide settings that used
-- to be .env-only, like the domain every module's URL is built from.
-- Editable from the web UI so an admin doesn't need shell access to
-- change it; base_domain still falls back to the BASE_DOMAIN env var on
-- first boot (see main.go), so existing deployments don't need to
-- re-enter it.
CREATE TABLE platform_settings (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    base_domain TEXT NOT NULL
);
