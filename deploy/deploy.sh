#!/usr/bin/env bash
# Builds and starts (or updates) Your Mate on the server.
#
#   deploy/deploy.sh            # deploy what's checked out
#   deploy/deploy.sh --pull     # git pull first, then deploy
#
# Services restart in dependency order with the gateway last, so it never
# spends half a minute retrying a backend that just moved.
set -euo pipefail
cd "$(dirname "$0")/.."

compose() { docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml "$@"; }

if [[ ! -f .env ]]; then
  echo "no .env yet: create it with deploy/make-env.sh (see deploy/README.md)" >&2
  exit 1
fi
set -a; . ./.env; set +a
for v in APP_DOMAIN ACME_EMAIL LIVEKIT_NODE_IP; do
  if [[ -z "${!v:-}" ]]; then echo "$v is missing from .env" >&2; exit 1; fi
done
if [[ "${DEV_MODE:-}" != "false" ]]; then
  echo "DEV_MODE must be false on a server" >&2
  exit 1
fi
if [[ -z "${SMTP_HOST:-}" ]]; then
  echo "warning: SMTP_HOST is not set, so signup codes are only logged, not emailed" >&2
fi

if [[ "${1:-}" == "--pull" ]]; then
  echo "== git pull"
  git pull --ff-only
fi

echo "== checking the configuration"
compose config -q

echo "== building images (a few minutes the first time)"
compose build --pull

echo "== database"
compose up -d postgres
for i in $(seq 1 30); do
  compose exec -T postgres pg_isready -U yourmate -q && break
  sleep 2
done

echo "== services"
compose up -d --remove-orphans identity marketplace community livekit
sleep 5
compose up -d gateway web caddy

echo "== waiting for https://${APP_DOMAIN}"
ok=""
for i in $(seq 1 40); do
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "https://${APP_DOMAIN}/" || true)"
  if [[ "$code" == "200" ]]; then ok=1; break; fi
  sleep 3
done
if [[ -z "$ok" ]]; then
  echo "the site didn't answer over HTTPS yet. Check that ${APP_DOMAIN} points to this server's IP," >&2
  echo "that ports 80 and 443 are open, then: docker compose logs caddy" >&2
  exit 1
fi
gql="$(curl -s --max-time 5 -H 'Content-Type: application/json' -d '{"query":"{ catalog { sessionDurations } }"}' "https://${APP_DOMAIN}/graphql")"
if [[ "$gql" != *sessionDurations* ]]; then
  echo "the API isn't answering: $gql" >&2
  exit 1
fi

echo "== tidying old images"
docker image prune -f >/dev/null

compose ps --format 'table {{.Service}}\t{{.Status}}'
echo
echo "Live at https://${APP_DOMAIN}"
