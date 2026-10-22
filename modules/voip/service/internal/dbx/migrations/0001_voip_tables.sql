-- One row per phone. username links it to a company (LDAP) account so that
-- employee sees their own setup in the portal; NULL for a shared phone
-- (reception, meeting room) that isn't anyone's in particular.
CREATE TABLE IF NOT EXISTS voip_extensions (
    number        TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    username      TEXT UNIQUE,
    password      TEXT NOT NULL,
    in_main_group BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Single row: the one outside line (SIP trunk) the whole office shares.
CREATE TABLE IF NOT EXISTS voip_settings (
    id                  BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    trunk_enabled       BOOLEAN NOT NULL DEFAULT false,
    trunk_host          TEXT NOT NULL DEFAULT '',
    trunk_username      TEXT NOT NULL DEFAULT '',
    trunk_password      TEXT NOT NULL DEFAULT '',
    trunk_caller_id     TEXT NOT NULL DEFAULT '',
    allow_international BOOLEAN NOT NULL DEFAULT false
);

INSERT INTO voip_settings (id) VALUES (true) ON CONFLICT DO NOTHING;
