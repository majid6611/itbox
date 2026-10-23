-- Optional 2FA for the VPN: when on, people get a login (password plus an
-- authenticator-app code) instead of a setup key. Off by default.
ALTER TABLE platform_settings ADD COLUMN vpn_two_factor BOOLEAN NOT NULL DEFAULT false;
-- The NetBird login created for a person in that mode ('' when they use a
-- setup key). The generated password is kept so the setup file can be
-- downloaded again, the same as the setup key is.
ALTER TABLE vpn_access ADD COLUMN nb_user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE vpn_access ADD COLUMN nb_password TEXT NOT NULL DEFAULT '';
