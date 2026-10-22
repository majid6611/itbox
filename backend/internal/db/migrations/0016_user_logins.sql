-- When each person last signed in to the employee portal, and from where.
-- Sessions are deleted on logout and expiry, so they can't answer "when did
-- this person last sign in" — this can.
CREATE TABLE user_logins (
    username TEXT PRIMARY KEY,
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_ip TEXT NOT NULL DEFAULT '',
    login_count INTEGER NOT NULL DEFAULT 1
);
