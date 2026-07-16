#!/usr/bin/env bash
# One-command install helper for Wernanmail full stack.
set -euo pipefail

REPO_URL="${WERNANMAIL_REPO_URL:-https://github.com/Baddysays/wernanmail.git}"
REPO_REF="${WERNANMAIL_REF:-main}"

if ! command -v git >/dev/null 2>&1; then
  echo "Git is required for the installer." >&2
  exit 1
fi

bootstrap_repo() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  echo "Fetching Wernanmail into a temporary directory..."
  git clone --depth 1 --branch "$REPO_REF" "$REPO_URL" "$tmp/repo"
  cd "$tmp/repo"
}

if [ -f "./docker-compose.yml" ] && [ -f "./.env.example" ]; then
  ROOT="$(pwd)"
elif [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/docker-compose.yml" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "$ROOT"
else
  bootstrap_repo
  ROOT="$(pwd)"
fi

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
