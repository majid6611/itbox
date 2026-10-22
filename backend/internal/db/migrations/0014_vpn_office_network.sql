-- The office LAN (a CIDR, e.g. 192.168.1.0/24) VPN users can reach through
-- the internal gateway — so a remote laptop or phone sees office computers,
-- printers and the phone system as if it were plugged in at the office.
-- Empty means off: VPN users reach only other VPN devices and the
-- platform's own private modules. See syncGatewayRoutes.
ALTER TABLE platform_settings ADD COLUMN vpn_office_network TEXT NOT NULL DEFAULT '';
