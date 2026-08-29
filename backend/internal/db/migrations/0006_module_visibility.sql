-- A module's primary route defaults to "private" — reachable only through
-- the internal gateway (VPN), not the public internet. "public" restores
-- today's behavior (reachable at the module's normal public hostname).
ALTER TABLE installed_modules ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';

-- Each module set to private gets its own fixed port on the internal
-- gateway's advertised address (10.201.28.2, nginx's static edge-network
-- IP) — one port per module, not shared, since there's no DNS for VPN
-- clients to tell modules apart by hostname the way the public path does.
CREATE TABLE module_private_ports (
    module_id TEXT PRIMARY KEY,
    port INT NOT NULL UNIQUE
);
