#!/usr/bin/env bash
# One-command install for Wernanmail (Docker Compose).
#
# What this does, in plain language:
#   1. Asks for your public mail hostname (or uses env / defaults)
#   2. Writes a .env file
#   3. Builds and starts the stack
#   4. Shows the admin password and what to do next
#   5. Optionally requests a Let's Encrypt certificate
#   6. Checks local listen/firewall ports and can open UFW/firewalld rules
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
    local esc
    esc="$(printf '%s' "$val" | sed 's/[\\/&|]/\\&/g')"
    sed "s|^${key}=.*|${key}=${esc}|" "$file" >"$tmp"
    mv "$tmp" "$file"
  else
    printf '%s=%s\n' "$key" "$val" >>"$file"
    rm -f "$tmp"
  fi
}

# Returns 0 if something is listening on TCP port locally.
port_listening() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -tlnH 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
    ss -tln 2>/dev/null | grep -qE "[:.]${port}[[:space:]]"
    return $?
  fi
  if command -v netstat >/dev/null 2>&1; then
    netstat -tln 2>/dev/null | grep -qE "[:.]${port}[[:space:]]"
    return $?
  fi
  return 2
}

detect_firewall() {
  if command -v ufw >/dev/null 2>&1; then
    if ufw status 2>/dev/null | grep -qi 'Status: active'; then
      echo ufw
      return
    fi
  fi
  if command -v firewall-cmd >/dev/null 2>&1; then
    if firewall-cmd --state 2>/dev/null | grep -qi running; then
      echo firewalld
      return
    fi
  fi
  echo none
}

firewall_allows_port() {
  local fw="$1"
  local port="$2"
  case "$fw" in
    ufw)
      ufw status 2>/dev/null | grep -E "^${port}(/tcp)?[[:space:]]+ALLOW" >/dev/null
      ;;
    firewalld)
      firewall-cmd --list-ports 2>/dev/null | tr ' ' '\n' | grep -qx "${port}/tcp"
      ;;
    *)
      return 1
      ;;
  esac
}

open_firewall_port() {
  local fw="$1"
  local port="$2"
  case "$fw" in
    ufw)
      if [ "$(id -u)" -eq 0 ]; then
        ufw allow "${port}/tcp" comment 'wernanmail' >/dev/null
      else
        sudo ufw allow "${port}/tcp" comment 'wernanmail' >/dev/null
      fi
      ;;
    firewalld)
      if [ "$(id -u)" -eq 0 ]; then
        firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null
        firewall-cmd --reload >/dev/null
      else
        sudo firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null
        sudo firewall-cmd --reload >/dev/null
      fi
      ;;
    *)
      return 1
      ;;
  esac
}

# After compose is up: show listen/firewall status and offer to open local firewall rules.
check_and_offer_ports() {
  local labels=(
    "25|SMTP inbound (receiving mail from the internet)"
    "80|HTTP (Let's Encrypt challenge)"
    "443|HTTPS (webmail + admin)"
    "587|Submission STARTTLS (sending mail from clients)"
    "143|IMAP STARTTLS (reading mail)"
    "465|SMTPS implicit TLS (optional — not bound by default Compose)"
    "993|IMAPS implicit TLS (optional — not bound by default Compose)"
  )
  local fw
  fw="$(detect_firewall)"

  echo
  echo "=== Ports (this machine) ==="
  echo "Listening = Docker/stack bound the port here."
  echo "Firewall  = local UFW/firewalld rule (NOT your cloud security group)."
  echo "465/993 are optional (IMAPS/SMTPS via TLS terminator); 'not listening' is normal on default Compose."
  echo

  local need_open=()
  local line port label listen_state fw_state
  for line in "${labels[@]}"; do
    port="${line%%|*}"
    label="${line#*|}"
    if port_listening "$port"; then
      listen_state="listening"
    else
      listen_state="not listening"
    fi
    case "$fw" in
      ufw|firewalld)
        if firewall_allows_port "$fw" "$port"; then
          fw_state="allowed in $fw"
        else
          fw_state="blocked/unknown in $fw"
          need_open+=("$port")
        fi
        ;;
      *)
        fw_state="no active ufw/firewalld detected"
        ;;
    esac
    printf "  %-4s  %-14s  %s\n" "$port" "$listen_state" "$fw_state"
    printf "         %s\n" "$label"
  done

  echo
  echo "Important limits:"
  echo "  • Cloud panels (Hetzner/AWS/… security groups) are separate — open ports there too."
  echo "  • Many VPS providers block port 25 until you ask support to unblock it."
  echo "    Local 'allow 25' does nothing if the provider filters it upstream."

  if [ "$fw" = "none" ]; then
    echo
    echo "No active UFW/firewalld — if you use a cloud firewall, open 25,80,443,587,143 there"
    echo "(plus 465/993 only if you expose IMAPS/SMTPS)."
    return
  fi

  if [ "${#need_open[@]}" -eq 0 ]; then
    echo
    echo "Local $fw already allows the mail/web ports we checked."
    return
  fi

  # Prefer opening the default Compose ports; ask separately for optional 465/993.
  local need_required=() need_optional=()
  local p
  for p in "${need_open[@]}"; do
    case "$p" in
      465|993) need_optional+=("$p") ;;
      *) need_required+=("$p") ;;
    esac
  done

  open_ports_list() {
    local list=("$@")
    local q
    for q in "${list[@]}"; do
      if open_firewall_port "$fw" "$q"; then
        echo "  opened ${q}/tcp in $fw"
      else
        echo "  could not open ${q}/tcp (try manually with sudo)"
      fi
    done
  }

  print_port_cmds() {
    local list=("$@")
    local q
    for q in "${list[@]}"; do
      if [ "$fw" = "ufw" ]; then
        echo "  sudo ufw allow ${q}/tcp"
      else
        echo "  sudo firewall-cmd --permanent --add-port=${q}/tcp && sudo firewall-cmd --reload"
      fi
    done
  }

  if ! want_interactive; then
    echo
    echo "Non-interactive: not changing firewall. Suggested $fw commands:"
    print_port_cmds "${need_open[@]}"
    return
  fi

  if [ "${#need_required[@]}" -gt 0 ]; then
    echo
    echo "Local $fw is active, but some required ports look closed: ${need_required[*]}"
    if prompt_yn "Open these ports in $fw now?" "y"; then
      open_ports_list "${need_required[@]}"
    else
      echo "Skipped. You can open them later, for example:"
      print_port_cmds "${need_required[@]}"
    fi
  fi

  if [ "${#need_optional[@]}" -gt 0 ]; then
    echo
    echo "Optional IMAPS/SMTPS ports are not allowed in $fw yet: ${need_optional[*]}"
    echo "(Default Compose uses 587/143 STARTTLS; open 465/993 only if a TLS terminator will listen there.)"
    if prompt_yn "Also open optional 465/993 in $fw?" "n"; then
      open_ports_list "${need_optional[@]}"
    else
      echo "Skipped optional ports."
    fi
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

check_and_offer_ports

echo
echo "=== What to do next (about 15 minutes) ==="
echo "1. Open Admin:  $PUBLIC_URL/admin/"
echo "2. Sign in, add your domain, generate DKIM."
echo "3. Open Overview → Setup checklist and DNS helper."
echo "   Publish MX, SPF, DKIM, DMARC, and PTR at your DNS panel."
echo "4. Confirm ports (above) + cloud firewall / provider port 25."
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
