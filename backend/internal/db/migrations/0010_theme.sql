-- Which of the two shipped color/type directions the whole platform uses
-- (both portals, admin-selected) — see Settings. Mirrors base_domain: a
-- single-row setting an admin can change from the web UI, defaulting to
-- "slate" so existing deployments render exactly as before this column
-- existed.
ALTER TABLE platform_settings ADD COLUMN theme TEXT NOT NULL DEFAULT 'slate';
