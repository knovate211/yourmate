#!/usr/bin/env bash
# Restores a backup made by backup.sh. This REPLACES the current database
# (and, if given, the uploaded files) with the backup's contents.
#
#   deploy/restore.sh /var/backups/yourmate/db-20261018-2130.dump [uploads-20261018-2130.tar.gz] --yes
set -euo pipefail
cd "$(dirname "$0")/.."

db="${1:?give the db-*.dump file to restore}"
files=""
confirm=""
for a in "${@:2}"; do
  case "$a" in
    --yes) confirm=1 ;;
    *) files="$a" ;;
  esac
done
[[ -f "$db" ]] || { echo "no such file: $db" >&2; exit 1; }
[[ -z "$files" || -f "$files" ]] || { echo "no such file: $files" >&2; exit 1; }
if [[ -z "$confirm" ]]; then
  echo "This replaces the live database${files:+ and uploads} with $db${files:+ and $files}."
  echo "Add --yes to go ahead." >&2
  exit 1
fi

compose() { docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml "$@"; }

echo "== stopping the app (the database stays up)"
compose stop caddy web gateway identity marketplace community

echo "== restoring the database"
compose exec -T postgres psql -U yourmate -d postgres -q -c "DROP DATABASE IF EXISTS yourmate WITH (FORCE)" -c "CREATE DATABASE yourmate OWNER yourmate"
compose exec -T postgres pg_restore -U yourmate -d yourmate --no-owner < "$db"

if [[ -n "$files" ]]; then
  echo "== restoring uploads"
  docker run --rm -v yourmate_uploads:/data -v "$(cd "$(dirname "$files")" && pwd)":/backup:ro alpine:3.22 \
    sh -c "rm -rf /data/* && tar -xzf /backup/$(basename "$files") -C /data"
fi

echo "== starting the app"
compose up -d identity marketplace community
sleep 5
compose up -d gateway web caddy
echo "Restored."
