#!/usr/bin/env bash
# Installs everything a fresh Ubuntu/Debian host needs to run this
# platform: Docker Engine + the Compose plugin. Nothing else — every
# other dependency (Go, Node, Postgres, nginx) runs inside containers.
# Safe to re-run; skips steps that are already done.
set -euo pipefail

if ! command -v apt-get >/dev/null 2>&1; then
  echo "This script only supports Ubuntu/Debian (needs apt-get)." >&2
  exit 1
fi

if command -v docker >/dev/null 2>&1; then
  echo "Docker is already installed: $(docker --version)"
else
  echo "Installing Docker..."
  sudo apt-get update
  sudo apt-get install -y ca-certificates curl gnupg

  sudo install -m 0755 -d /etc/apt/keyrings
  . /etc/os-release
  curl -fsSL "https://download.docker.com/linux/${ID}/gpg" | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg

  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

sudo systemctl enable --now docker

# Lets the deploying user run `docker`/`docker compose` without sudo —
# takes effect on next login (or after `newgrp docker`).
if ! id -nG "$USER" | grep -qw docker; then
  sudo usermod -aG docker "$USER"
  echo "Added $USER to the docker group — log out and back in (or run 'newgrp docker') for it to take effect."
fi

# git and rsync: not needed to run the platform, but needed to get its
# code onto this host for the deploy step.
sudo apt-get install -y git rsync

echo
echo "Done."
docker --version
docker compose version
