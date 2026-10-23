-- What VPN users may use on the office network: '' means everything, otherwise
-- a comma-separated list like "tcp/445,tcp/3389,udp/20000-20999,icmp".
ALTER TABLE platform_settings ADD COLUMN vpn_office_ports TEXT NOT NULL DEFAULT '';
