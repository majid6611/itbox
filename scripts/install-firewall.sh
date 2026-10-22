#!/usr/bin/env bash
# Installs the host firewall (firewall.nft) and makes it load at every boot,
# after Docker. Safe to re-run — also how a port-list change is applied.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  exec sudo "$0" "$@"
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v nft >/dev/null 2>&1; then
  echo "Installing nftables..."
  apt-get install -y nftables
fi

# Check the rules parse before touching the running firewall.
nft -c -f "$HERE/firewall.nft"

mkdir -p /etc/itbox
install -m 0644 "$HERE/firewall.nft" /etc/itbox/firewall.nft

cat > /etc/systemd/system/itbox-firewall.service <<'UNIT'
[Unit]
Description=IT platform host firewall
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/itbox/firewall.nft
ExecReload=/usr/sbin/nft -f /etc/itbox/firewall.nft
ExecStop=-/usr/sbin/nft delete table inet itbox_fw

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable itbox-firewall.service >/dev/null
systemctl restart itbox-firewall.service
echo "Firewall active. Remove it any time with: sudo systemctl disable --now itbox-firewall"
