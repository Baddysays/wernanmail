#!/usr/bin/env bash
# Daily full backup with simple retention (default: keep 7 archives).
# Intended for cron / systemd timer on a native install.
#
# Env:
#   DATA_DIR     default /opt/wernanmail/data
#   BACKUP_DIR   default /var/backups/wernanmail
#   KEEP_DAYS    default 7
#   ROOT         install root (for locating backup-data.sh)
set -euo pipefail

ROOT="${ROOT:-/opt/wernanmail}"
DATA_DIR="${DATA_DIR:-$ROOT/data}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/wernanmail}"
KEEP_DAYS="${KEEP_DAYS:-7}"
SCRIPT="$ROOT/scripts/backup-data.sh"

if [ ! -x "$SCRIPT" ]; then
  echo "backup-data.sh not found or not executable: $SCRIPT" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"
OUT="$BACKUP_DIR/mail-$(date -u +%Y%m%d-%H%M%S).tgz"
"$SCRIPT" "$DATA_DIR" "$OUT"

# Drop archives older than KEEP_DAYS (by mtime).
find "$BACKUP_DIR" -maxdepth 1 -type f -name 'mail-*.tgz' -mtime +"$KEEP_DAYS" -delete 2>/dev/null || true
ls -lh "$OUT"
