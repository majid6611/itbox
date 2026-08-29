-- Tracks which LDAP users have been given VPN enrollment. NetBird's setup
-- keys are the simple, non-technical-friendly enrollment path (paste a
-- code into the app once, no browser login) — this table lets the panel
-- show who has one and re-serve the same key without calling the NetBird
-- API again, and lets us revoke it on disable.
CREATE TABLE vpn_access (
    username TEXT PRIMARY KEY,
    setup_key_id TEXT NOT NULL,
    setup_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
