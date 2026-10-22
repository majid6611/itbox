#!/bin/sh
# Everything static lives in /etc/asterisk (baked into the image). The one
# thing that isn't is manager.conf — it carries the AMI secret the voip
# service logs in with, which only exists at install time. Everything the
# admin actually manages (extensions, main number, outside line) is
# written by the voip service into /etc/asterisk/itp and pulled in with
# #tryinclude, so a fresh install with nothing configured yet still boots.
set -e
: "${AMI_SECRET:?AMI_SECRET is required}"

# The manager port listens only on this host's address on the platform's
# edge network (the bridge's gateway) — reachable from the voip container,
# never from the LAN. Found by looking, not hardcoded: Docker picks the
# gateway from the network's ip_range, so it isn't simply .1.
EDGE_PREFIX="${EDGE_PREFIX:-10.201.28.}"
if [ -z "$AMI_BIND" ]; then
  AMI_BIND=$(ip -4 -o addr show | awk '{print $4}' | cut -d/ -f1 | awk -v p="$EDGE_PREFIX" 'index($0, p) == 1' | head -1)
fi
if [ -z "$AMI_BIND" ]; then
  echo "no ${EDGE_PREFIX}x address on this host — is the platform's edge network up?" >&2
  exit 1
fi

cat > /etc/asterisk/manager.conf <<EOF
[general]
enabled = yes
port = 5038
bindaddr = ${AMI_BIND}

[itp]
secret = ${AMI_SECRET}
deny = 0.0.0.0/0.0.0.0
permit = ${EDGE_PREFIX}0/255.255.255.0
read = system,call,reporting
write = system,command,reporting
EOF

# The address phones should register to — this host's own LAN IP (the
# source address it would use to reach the outside), visible here because
# this container shares the host's network. The voip service reads it
# back over AMI (ENV(SERVER_IP)) to show in phone setup instructions.
# SERVER_IP set explicitly wins, for hosts where the guess is wrong.
if [ -z "$SERVER_IP" ]; then
  SERVER_IP=$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit }}')
fi
export SERVER_IP

# SIP and audio bound to that one address, not 0.0.0.0. This host sits on
# several networks (LAN, Docker bridges), and a 0.0.0.0 UDP socket lets
# the kernel pick each reply's source address by route — so a phone that
# sent to the LAN IP can get its answer from the host's Docker-bridge
# address instead. Fine on a flat LAN, but anything NATing in between
# (the VPN's internal gateway, for one) drops replies from an address it
# never sent to — VPN phones got no answer to REGISTER at all until this
# was pinned. Every generated phone/trunk endpoint inherits the
# itp-endpoint template, which does the same for audio (RTP).
BIND_IP="${SERVER_IP:-0.0.0.0}"
cat > /etc/asterisk/pjsip_local.conf <<EOF
[transport-udp]
type = transport
protocol = udp
bind = ${BIND_IP}:5060

[itp-endpoint](!)
type = endpoint
EOF
if [ -n "$SERVER_IP" ]; then
  cat >> /etc/asterisk/pjsip_local.conf <<EOF
media_address = ${SERVER_IP}
bind_rtp_to_media_address = yes
EOF
fi

mkdir -p /var/log/asterisk/cdr-csv /var/spool/asterisk/voicemail /var/run/asterisk /var/lib/asterisk
chown -R asterisk:asterisk /var/log/asterisk /var/spool/asterisk /var/run/asterisk /var/lib/asterisk

exec asterisk -f -vvv -U asterisk -G asterisk
