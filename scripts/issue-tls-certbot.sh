#!/usr/bin/env bash
# Get a real HTTPS certificate (Let's Encrypt) and put it into the Docker TLS volume.
#
# Plain-language overview:
#   - Your DNS name (MAIL_HOSTNAME) must already point at this server.
#   - Port 80 must be reachable from the internet (Certbot checks ownership).
#   - This replaces the temporary self-signed certificate from first boot.
#
# Usage (on the Docker host, from the repo root):
#   MAIL_HOSTNAME=mail.example.com ./scripts/issue-tls-certbot.sh
#   # or after install.sh wrote .env:
#   ./scripts/issue-tls-certbot.sh
#
# Requires: certbot, docker compose
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${MAIL_HOSTNAME:-}"
EMAIL_FROM_ENV="${CERTBOT_EMAIL:-}"
if [ -z "$HOST" ] && [ -f .env ]; then
  # shellcheck disable=SC1091
  set -a; . ./.env; set +a
  HOST="${MAIL_HOSTNAME:-}"
  EMAIL_FROM_ENV="${CERTBOT_EMAIL:-$EMAIL_FROM_ENV}"
fi
if [ -z "$HOST" ] || [ "$HOST" = "localhost" ]; then
  echo "Set MAIL_HOSTNAME to your public mail host (not localhost)." >&2
  echo "Example: MAIL_HOSTNAME=mail.example.com $0" >&2
  exit 1
fi

EMAIL="${EMAIL_FROM_ENV:-admin@$HOST}"
WEBROOT="${CERTBOT_WEBROOT:-/var/www/certbot}"
LIVE_DIR="${LIVE_DIR:-/etc/letsencrypt/live/$HOST}"

if [ ! -f "$LIVE_DIR/fullchain.pem" ] || [ ! -f "$LIVE_DIR/privkey.pem" ]; then
  if ! command -v certbot >/dev/null 2>&1; then
    echo "certbot is not installed. Example: sudo apt install certbot" >&2
    exit 1
  fi
  mkdir -p "$WEBROOT"
  echo "Requesting certificate for $HOST (notices → $EMAIL)…"
  echo "Make sure DNS A/AAAA for $HOST points here and port 80 is open."
  certbot certonly --webroot -w "$WEBROOT" -d "$HOST" --agree-tos -m "$EMAIL" --non-interactive
fi

if [ ! -f "$LIVE_DIR/fullchain.pem" ] || [ ! -f "$LIVE_DIR/privkey.pem" ]; then
  echo "Missing $LIVE_DIR/{fullchain,privkey}.pem" >&2
  exit 1
fi

echo "Installing certificate into the Docker mail_tls volume…"
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

echo "Restarting services that use TLS…"
docker compose restart mta imapd web

echo
echo "Done. Check in a browser: https://$HOST/"
echo "If it still warns, wait a minute for restart, or hard-refresh the page."
echo "Renew later: certbot renew && LIVE_DIR=$LIVE_DIR $0"
