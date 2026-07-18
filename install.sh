#!/usr/bin/env bash
# One-command install for Wernanmail (Docker Compose).
#
# What this does, in plain language:
#   1. Asks for your public mail hostname (or uses env / defaults)
#   2. Writes a .env file
#   3. Builds and starts the stack
#   4. Shows the admin password and what to do next
#   5. Optionally requests a Let's Encrypt certificate
#
# Non-interactive (CI / scripts):
#   WERNANMAIL_NONINTERACTIVE=1 MAIL_HOSTNAME=mail.example.com ./install.sh
#
set -euo pipefail

REPO_URL="${WERNANMAIL_REPO_URL:-https://github.com/Baddysays/wernanmail.git}"
REPO_REF="${WERNANMAIL_REF:-main}"

is_tty() {
  [ -t 0 ] && [ -t 1 ]
}

want_interactive() {
  if [ "${WERNANMAIL_NONINTERACTIVE:-}" = "1" ] || [ "${CI:-}" = "true" ]; then
    return 1
  fi
  is_tty
}

prompt() {
  # prompt "Question" "default" → echoes answer
  local question="$1"
  local default="${2:-}"
  local answer=""
  if [ -n "$default" ]; then
    read -r -p "$question [$default]: " answer || true
    echo "${answer:-$default}"
  else
    read -r -p "$question: " answer || true
    echo "$answer"
  fi
}

prompt_yn() {
  local question="$1"
  local default="${2:-n}"
  local answer=""
  if [ "$default" = "y" ]; then
    read -r -p "$question [Y/n]: " answer || true
    answer="${answer:-y}"
  else
    read -r -p "$question [y/N]: " answer || true
    answer="${answer:-n}"
  fi
  case "$(echo "$answer" | tr '[:upper:]' '[:lower:]')" in
    y|yes) return 0 ;;
    *) return 1 ;;
  esac
}

set_env_var() {
  local key="$1"
  local val="$2"
  local file="${3:-.env}"
  local tmp
  tmp="$(mktemp)"
  if [ -f "$file" ] && grep -qE "^${key}=" "$file"; then
    # Escape | in value for sed
    local esc
    esc="$(printf '%s' "$val" | sed 's/[\\/&|]/\\&/g')"
    sed "s|^${key}=.*|${key}=${esc}|" "$file" >"$tmp"
    mv "$tmp" "$file"
  else
    printf '%s=%s\n' "$key" "$val" >>"$file"
    rm -f "$tmp"
  fi
}

if ! command -v git >/dev/null 2>&1; then
  echo "Git is required. Install git, then run this again." >&2
  exit 1
fi

bootstrap_repo() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  echo "Downloading Wernanmail…"
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
  echo "Docker Compose v2 is required (command: docker compose)." >&2
  exit 1
fi

if [ ! -f .env ] && [ -f .env.example ]; then
  cp .env.example .env
fi
if [ ! -f .env ]; then
  echo "Missing .env and .env.example — cannot continue." >&2
  exit 1
fi

# Load existing values as defaults (without printing secrets).
set -a
# shellcheck disable=SC1091
. ./.env
set +a

MAIL_HOSTNAME="${MAIL_HOSTNAME:-localhost}"
MAIL_EHLO="${MAIL_EHLO:-$MAIL_HOSTNAME}"
PUBLIC_URL="${PUBLIC_URL:-https://localhost}"
CERTBOT_EMAIL="${CERTBOT_EMAIL:-}"

echo
echo "=== Wernanmail setup ==="
echo "This configures your public mail hostname, starts the stack,"
echo "and tells you how to finish DNS + TLS so mail can leave Spam."
echo

if want_interactive; then
  echo "Use a real DNS name that points at this server (A/AAAA),"
  echo "for example: mail.example.com — not localhost."
  echo
  MAIL_HOSTNAME="$(prompt "Mail hostname (for MX / HTTPS)" "$MAIL_HOSTNAME")"
  MAIL_EHLO="$(prompt "SMTP EHLO name (usually same as hostname)" "${MAIL_EHLO:-$MAIL_HOSTNAME}")"
  if [ "$PUBLIC_URL" = "https://localhost" ] || [ -z "$PUBLIC_URL" ]; then
    PUBLIC_URL="https://${MAIL_HOSTNAME}"
  fi
  PUBLIC_URL="$(prompt "Public web URL (browser address)" "$PUBLIC_URL")"
  if [ -z "$CERTBOT_EMAIL" ]; then
    CERTBOT_EMAIL="admin@${MAIL_HOSTNAME#mail.}"
    case "$CERTBOT_EMAIL" in
      admin@localhost|admin@) CERTBOT_EMAIL="admin@${MAIL_HOSTNAME}" ;;
    esac
  fi
  CERTBOT_EMAIL="$(prompt "Email for Let's Encrypt notices (and admin contact)" "$CERTBOT_EMAIL")"
else
  echo "Non-interactive mode — using .env / environment values."
  echo "  MAIL_HOSTNAME=$MAIL_HOSTNAME"
  echo "  PUBLIC_URL=$PUBLIC_URL"
fi

set_env_var MAIL_HOSTNAME "$MAIL_HOSTNAME"
set_env_var MAIL_EHLO "$MAIL_EHLO"
set_env_var PUBLIC_URL "$PUBLIC_URL"
if [ -n "$CERTBOT_EMAIL" ]; then
  set_env_var CERTBOT_EMAIL "$CERTBOT_EMAIL"
fi

echo
echo "Starting Wernanmail (first build can take a few minutes)…"
docker compose up --build -d

echo
echo "=== Stack is up ==="
echo "  Webmail:  $PUBLIC_URL/"
echo "  Admin:    $PUBLIC_URL/admin/"
echo
echo "Admin login user: ${ADMIN_USER:-admin}"
echo -n "Admin password: "
docker compose run --rm init /app/docker-init show-admin-password 2>/dev/null | sed -n 's/^admin password: //p' || echo "(run: docker compose run --rm init /app/docker-init show-admin-password)"
echo

ISSUE_TLS=0
if [ "$MAIL_HOSTNAME" != "localhost" ] && [ "$MAIL_HOSTNAME" != "127.0.0.1" ]; then
  if want_interactive; then
    echo "Browsers will warn about the temporary self-signed certificate."
    if prompt_yn "Request a free Let's Encrypt certificate for $MAIL_HOSTNAME now?" "y"; then
      ISSUE_TLS=1
    fi
  elif [ "${WERNANMAIL_ISSUE_TLS:-}" = "1" ]; then
    ISSUE_TLS=1
  fi
fi

if [ "$ISSUE_TLS" = "1" ]; then
  if ! command -v certbot >/dev/null 2>&1; then
    echo
    echo "certbot is not installed on this host."
    echo "Install it (e.g. apt install certbot), open port 80 to the world,"
    echo "then run:"
    echo "  MAIL_HOSTNAME=$MAIL_HOSTNAME CERTBOT_EMAIL=$CERTBOT_EMAIL ./scripts/issue-tls-certbot.sh"
  else
    echo
    echo "Requesting Let's Encrypt certificate…"
    echo "(Port 80 must reach this server; DNS A/AAAA for $MAIL_HOSTNAME must already point here.)"
    if MAIL_HOSTNAME="$MAIL_HOSTNAME" CERTBOT_EMAIL="$CERTBOT_EMAIL" ./scripts/issue-tls-certbot.sh; then
      echo "TLS certificate installed."
    else
      echo
      echo "TLS step failed — the stack is still running with a temporary certificate."
      echo "Fix DNS / port 80, then run:"
      echo "  ./scripts/issue-tls-certbot.sh"
    fi
  fi
fi

echo
echo "=== What to do next (about 15 minutes) ==="
echo "1. Open Admin:  $PUBLIC_URL/admin/"
echo "2. Sign in, add your domain, generate DKIM."
echo "3. Open Overview → Setup checklist and DNS helper."
echo "   Publish MX, SPF, DKIM, DMARC, and PTR at your DNS panel."
echo "4. Open firewall ports: 25, 587, 143, 80, 443."
echo "5. Send a test message and watch Deliverability score."
echo
echo "If the browser warns about the certificate, either finish Let's Encrypt"
echo "(./scripts/issue-tls-certbot.sh) or continue only for local testing."
echo
echo "Useful commands:"
echo "  docker compose ps"
echo "  docker compose logs -f"
echo "  docker compose run --rm init /app/docker-init show-admin-password"
echo "  ./scripts/issue-tls-certbot.sh"
echo "  ./scripts/backup-data.sh"
echo
echo "Full guide: docs/SERVER.md"
echo "Done."
