#!/usr/bin/env bash
# One-command install helper for Wernanmail full stack.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required. Install Docker Engine + Compose v2, then re-run." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose v2 is required (docker compose)." >&2
  exit 1
fi

if [ ! -f .env ] && [ -f .env.example ]; then
  cp .env.example .env
  echo "Created .env from .env.example — edit MAIL_HOSTNAME / PUBLIC_URL for production."
fi

echo "Building and starting Wernanmail..."
docker compose up --build -d

echo
echo "Stack is up."
echo "  Webmail:  https://localhost/   (or PUBLIC_URL)"
echo "  Admin:    https://localhost/admin/"
echo
echo "Admin password:"
docker compose run --rm init /app/docker-init show-admin-password || true
echo
echo "Useful: docker compose ps | logs -f | down"
echo "CI smoke:   ./scripts/docker-smoke.sh"
