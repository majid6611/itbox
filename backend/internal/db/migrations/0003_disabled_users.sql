-- Tracks which LDAP-managed users are disabled. LDAP itself has no simple
-- "locked" flag without the ppolicy overlay (which isn't configured on
-- the running directory, and enabling it now would need a fresh install —
-- LDAP_INIT_* vars only apply on first boot with empty volumes). Instead,
-- "disable" resets the user's LDAP password to a value nobody is shown
-- (revoking their ability to authenticate) and removes their WebDAV
-- login; this table just records who that's been done to, so the panel
-- can show status and offer "Enable" (which sets a fresh password).
CREATE TABLE disabled_users (
    username TEXT PRIMARY KEY,
    disabled_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
