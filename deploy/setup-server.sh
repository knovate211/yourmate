#!/usr/bin/env bash
# One-time preparation of a fresh Ubuntu 24.04 server (x86 or ARM):
# Docker from Docker's own repository, swap, a firewall, automatic security
# updates, and the nightly backup. Run as the default user with sudo:
#
#   sudo bash deploy/setup-server.sh
#
# Safe to run again; each step checks whether it's already done.
set -euo pipefail
cd "$(dirname "$0")/.."
repo="$(pwd)"
owner="${SUDO_USER:-$(id -un)}"

if [[ $EUID -ne 0 ]]; then
  echo "run with sudo" >&2
  exit 1
fi

echo "== packages"
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg ufw unattended-upgrades openssl >/dev/null

echo "== Docker"
if ! command -v docker >/dev/null; then
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  . /etc/os-release
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null
fi
usermod -aG docker "$owner"
systemctl enable --now docker >/dev/null

echo "== swap (helps image builds on small servers)"
if ! swapon --show | grep -q /swapfile; then
  fallocate -l 2G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

echo "== firewall"
# The cloud firewall (AWS security group) must allow the same ports.
ufw allow OpenSSH >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw allow 443/udp >/dev/null
ufw allow 7881/tcp >/dev/null
ufw allow 52000:52100/udp >/dev/null
ufw --force enable >/dev/null

echo "== automatic security updates"
dpkg-reconfigure -f noninteractive unattended-upgrades >/dev/null

echo "== nightly backup at 03:00 India time (21:30 UTC)"
mkdir -p /var/backups/yourmate
chown "$owner" /var/backups/yourmate
cat > /etc/cron.d/yourmate-backup <<CRON
# Your Mate: database and uploads, kept for 14 days. See deploy/backup.sh.
30 21 * * * ${owner} ${repo}/deploy/backup.sh >> /var/backups/yourmate/backup.log 2>&1
CRON
chmod 644 /etc/cron.d/yourmate-backup

echo
echo "Done. Log out and back in (so '${owner}' can use docker), then:"
echo "  deploy/make-env.sh   (once, see deploy/README.md)"
echo "  deploy/deploy.sh"
