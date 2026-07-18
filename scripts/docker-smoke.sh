#!/usr/bin/env bash
# Bring up the full Compose stack on non-privileged ports and verify health.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker: not found" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose v2: not found" >&2
  exit 1
fi

export MAIL_HOSTNAME="${MAIL_HOSTNAME:-localhost}"
export MAIL_EHLO="${MAIL_EHLO:-localhost}"
export PUBLIC_URL="${PUBLIC_URL:-https://localhost}"
export SMTP_PORT="${SMTP_PORT:-2525}"
export SUBMISSION_PORT="${SUBMISSION_PORT:-2587}"
export SMTPS_PORT="${SMTPS_PORT:-2465}"
export IMAP_PORT="${IMAP_PORT:-2143}"
export IMAPS_PORT="${IMAPS_PORT:-2993}"
export HTTP_PORT="${HTTP_PORT:-8088}"
export HTTPS_PORT="${HTTPS_PORT:-8443}"
export COOKIE_SECURE="${COOKIE_SECURE:-false}"

cleanup() {
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Building and starting stack (host ports ${HTTP_PORT}/${HTTPS_PORT}, SMTP ${SMTP_PORT})..."
docker compose up --build -d

echo "Waiting for web service to become healthy (up to 6 minutes)..."
if docker compose wait web --timeout 360 2>/dev/null; then
  echo "docker compose wait: web healthy"
else
  echo "docker compose wait unavailable or timed out; polling healthchecks..."
  deadline=$((SECONDS + 360))
  while [ "$SECONDS" -lt "$deadline" ]; do
    unhealthy="$(docker compose ps --format '{{.Name}} {{.Health}}' | awk '$2 != "healthy" && $2 != "" {print}')"
    if [ -z "$unhealthy" ]; then
      running="$(docker compose ps --status running -q | wc -l | tr -d ' ')"
      if [ "${running:-0}" -ge 6 ]; then
        break
      fi
    fi
    sleep 5
  done
fi

docker compose ps

echo "Checking HTTPS healthz..."
curl -fsSk "https://127.0.0.1:${HTTPS_PORT}/healthz" >/dev/null

echo "Checking readiness..."
curl -fsSk "https://127.0.0.1:${HTTPS_PORT}/readyz" | grep -q '"status"' || {
  echo "readyz missing status" >&2
  exit 1
}

echo "Checking Prometheus metrics (loopback inside admin; edge /metrics stays deny-by-default)..."
# Host→published HTTPS sees the Docker bridge IP, so nginx correctly returns 403.
edge_code="$(curl -sk -o /dev/null -w '%{http_code}' "https://127.0.0.1:${HTTPS_PORT}/metrics" || true)"
if [ "$edge_code" != "403" ]; then
  echo "edge /metrics HTTP status: $edge_code (expected 403)" >&2
  exit 1
fi
metrics="$(docker compose exec -T admin wget -qO- http://127.0.0.1:8090/metrics)"
echo "$metrics" | grep -q 'wernanmail_up{process="admin"} 1' || {
  echo "metrics missing wernanmail_up admin" >&2
  exit 1
}
echo "$metrics" | grep -q 'wernanmail_queue_pending' || {
  echo "metrics missing queue_pending" >&2
  exit 1
}

echo "Checking admin SPA..."
code="$(curl -fsSk -o /dev/null -w '%{http_code}' "https://127.0.0.1:${HTTPS_PORT}/admin/")"
if [ "$code" != "200" ]; then
  echo "admin HTTP status: $code (expected 200)" >&2
  exit 1
fi

echo "Checking generated admin password..."
docker compose run --rm init /app/docker-init show-admin-password | grep -q 'admin password:'

echo "Docker smoke passed."
