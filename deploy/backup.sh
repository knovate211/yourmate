#!/usr/bin/env bash
# Backs up the database and the uploaded files, keeping 14 days.
# Runs nightly from cron (installed by setup-server.sh); safe to run by hand.
#
# Optional: set BACKUP_S3_BUCKET in .env (and install the AWS CLI with
# access to the bucket) to also copy each backup off the server.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

dir="${BACKUP_DIR:-/var/backups/yourmate}"
keep_days="${BACKUP_KEEP_DAYS:-14}"
stamp="$(date -u +%Y%m%d-%H%M)"
mkdir -p "$dir"

compose() { docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml "$@"; }

# Database: pg_dump's custom format, compressed; restores with pg_restore.
db="$dir/db-$stamp.dump"
compose exec -T postgres pg_dump -U yourmate -d yourmate -Fc > "$db.part"
mv "$db.part" "$db"

# Uploads (avatars, post images, ID documents awaiting review, resumes).
files="$dir/uploads-$stamp.tar.gz"
docker run --rm -v yourmate_uploads:/data:ro -v "$dir":/backup alpine:3.22 \
  tar -czf "/backup/uploads-$stamp.tar.gz.part" -C /data .
mv "$files.part" "$files"
chmod 600 "$db" "$files"

if [[ -n "${BACKUP_S3_BUCKET:-}" ]]; then
  aws s3 cp "$db" "s3://${BACKUP_S3_BUCKET}/yourmate/" --only-show-errors
  aws s3 cp "$files" "s3://${BACKUP_S3_BUCKET}/yourmate/" --only-show-errors
fi

find "$dir" -name 'db-*.dump' -mtime +"$keep_days" -delete
find "$dir" -name 'uploads-*.tar.gz' -mtime +"$keep_days" -delete
echo "$(date -u +%FT%TZ) backup ok: $(du -h "$db" | cut -f1) database, $(du -h "$files" | cut -f1) uploads"
