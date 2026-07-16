#!/usr/bin/env bash
# Issue / renew Let's Encrypt certs and install them into the Compose TLS volume.
#
# Host-level Certbot remains the v1 path (no ACME inside the MTA).
#
# Usage (on the Docker host):
#   MAIL_HOSTNAME=mail.example.com ./scripts/issue-tls-certbot.sh
#
# Requires: certbot, docker compose, HTTP-01 reachable on :80
# (or obtain the cert yourself and point LIVE_DIR at it).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${MAIL_HOSTNAME:-}"
if [ -z "$HOST" ] && [ -f .env ]; then
  # shellcheck disable=SC1091
  set -a; . ./.env; set +a
  HOST="${MAIL_HOSTNAME:-}"
fi
if [ -z "$HOST" ] || [ "$HOST" = "localhost" ]; then
  echo "Set MAIL_HOSTNAME to your public mail host (not localhost)." >&2
  exit 1
fi

EMAIL="${CERTBOT_EMAIL:-admin@$HOST}"
WEBROOT="${CERTBOT_WEBROOT:-/var/www/certbot}"
LIVE_DIR="${LIVE_DIR:-/etc/letsencrypt/live/$HOST}"

if [ ! -f "$LIVE_DIR/fullchain.pem" ] || [ ! -f "$LIVE_DIR/privkey.pem" ]; then
  mkdir -p "$WEBROOT"
  echo "Requesting certificate for $HOST (email=$EMAIL)..."
  certbot certonly --webroot -w "$WEBROOT" -d "$HOST" --agree-tos -m "$EMAIL" --non-interactive
fi

if [ ! -f "$LIVE_DIR/fullchain.pem" ] || [ ! -f "$LIVE_DIR/privkey.pem" ]; then
  echo "Missing $LIVE_DIR/{fullchain,privkey}.pem" >&2
  exit 1
fi

echo "Installing certs into Docker volume mail_tls..."
docker compose run --rm --user 0:0 \
  -v "$LIVE_DIR:/certs:ro" \
  --entrypoint sh init -c '
    mkdir -p /run/tls
    cp -f /certs/fullchain.pem /run/tls/fullchain.pem
    cp -f /certs/privkey.pem /run/tls/privkey.pem
    chown 10001:10001 /run/tls/fullchain.pem /run/tls/privkey.pem
    chmod 400 /run/tls/fullchain.pem /run/tls/privkey.pem
    ls -la /run/tls
  '

echo "Restarting TLS consumers..."
docker compose restart mta imapd web

echo "Done. Verify: curl -vk https://$HOST/healthz"
echo "Renew later: certbot renew && LIVE_DIR=$LIVE_DIR $0"
