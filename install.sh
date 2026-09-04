#!/usr/bin/env bash
# One-command setup: installs Docker if it's missing, asks for the handful
# of things only a human can decide (admin login, the domain this is
# reached at), generates everything else, and starts the platform. Safe
# to re-run — an existing .env is left alone unless you explicitly choose
# to redo it, so this also works as "just start the thing" on a box
# that's already set up.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

echo "== IT Platform installer =="
echo

# --- 1. Docker -------------------------------------------------------------
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  echo "Docker is already installed: $(docker --version)"
else
  echo "Installing Docker..."
  "$REPO_ROOT/scripts/install-docker.sh"
  echo
  echo "Docker was just installed and your user was added to the docker group."
  echo "That needs a fresh login to take effect. Log out and back in (or run"
  echo "'newgrp docker' in this shell), then run this script again to continue."
  exit 0
fi
echo

# --- 2. Configuration --------------------------------------------------------
if [ -f "$REPO_ROOT/.env" ]; then
  read -r -p ".env already exists — keep it and just (re)start? [Y/n] " keep_env
  keep_env=${keep_env:-Y}
  if [[ "$keep_env" =~ ^[Yy] ]]; then
    echo "Keeping existing .env."
    SKIP_CONFIG=1
  fi
fi

if [ -z "${SKIP_CONFIG:-}" ]; then
  echo "-- Admin account --"
  read -r -p "Admin email [admin@example.com]: " admin_email
  admin_email=${admin_email:-admin@example.com}

  while true; do
    read -r -s -p "Admin password (min. 8 characters): " admin_password
    echo
    if [ "${#admin_password}" -lt 8 ]; then
      echo "Too short — try again."
      continue
    fi
    read -r -s -p "Confirm password: " admin_password_confirm
    echo
    if [ "$admin_password" != "$admin_password_confirm" ]; then
      echo "Those didn't match — try again."
      continue
    fi
    break
  done

  echo
  echo "-- Domain --"
  echo "Leave this as localhost to try it out on this machine. If you already"
  echo "have a real domain pointed at this server, enter it now (e.g."
  echo "it.yourcompany.com) — every module gets its own subdomain under it."
  read -r -p "Base domain [localhost]: " base_domain
  base_domain=${base_domain:-localhost}

  # Nobody ever needs to know or type this one.
  postgres_password=$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)

  cat > "$REPO_ROOT/.env" <<EOF
POSTGRES_PASSWORD=${postgres_password}
BASE_DOMAIN=${base_domain}
ADMIN_EMAIL=${admin_email}
ADMIN_PASSWORD=${admin_password}
MODULE_REGISTRY_URL=
MODULE_REGISTRY_KEY=
EOF
  echo
  echo ".env written."
fi
echo

# --- 3. Start ----------------------------------------------------------------
echo "Starting the platform (this pulls/builds images the first time, which"
echo "can take a few minutes)..."
docker compose up -d

echo
echo -n "Waiting for it to come up"
base_domain=$(grep -E '^BASE_DOMAIN=' "$REPO_ROOT/.env" | cut -d= -f2-)
ready=0
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://localhost:8000/api/health" 2>/dev/null; then
    ready=1
    break
  fi
  echo -n "."
  sleep 3
done
echo

if [ "$ready" -eq 1 ]; then
  echo "== Ready =="
  echo "Open http://${base_domain}:8000 (or just http://localhost:8000 on this machine) and log in with the admin email/password you just set."
else
  echo "Still starting — 'docker compose logs -f backend' to watch it, or 'docker compose ps' to check container status."
fi
