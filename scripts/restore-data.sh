#!/usr/bin/env bash
# Restore full data backup created by backup-data.sh.
# Usage:
#   ./scripts/restore-data.sh BACKUP.tar.gz [DATA_DIR]
#
# Stop mail processes first (docker compose stop / ./run.sh stop).
set -euo pipefail

ARCHIVE="${1:-}"
DATA_DIR="${2:-${DATA_DIR:-./data}}"

if [ -z "$ARCHIVE" ] || [ ! -f "$ARCHIVE" ]; then
  echo "Usage: $0 BACKUP.tar.gz [DATA_DIR]" >&2
  exit 1
fi

if [ "${WERNANMAIL_RESTORE_CONFIRM:-}" != "yes" ]; then
  echo "This replaces $DATA_DIR/mail.db and $DATA_DIR/maildir." >&2
  echo "Stop the stack, then re-run with WERNANMAIL_RESTORE_CONFIRM=yes" >&2
  exit 1
fi

mkdir -p "$DATA_DIR"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
tar -C "$TMP" -xzf "$ARCHIVE"

if [ ! -f "$TMP/mail.db" ]; then
  echo "archive missing mail.db" >&2
  exit 1
fi

STAMP="$(date -u +%Y%m%d-%H%M%S)"
if [ -f "$DATA_DIR/mail.db" ]; then
  mv -f "$DATA_DIR/mail.db" "$DATA_DIR/mail.db.bak-$STAMP"
fi
if [ -d "$DATA_DIR/maildir" ]; then
  mv -f "$DATA_DIR/maildir" "$DATA_DIR/maildir.bak-$STAMP"
fi
rm -f "$DATA_DIR/mail.db-wal" "$DATA_DIR/mail.db-shm" 2>/dev/null || true

cp -f "$TMP/mail.db" "$DATA_DIR/mail.db"
if [ -d "$TMP/maildir" ]; then
  cp -a "$TMP/maildir" "$DATA_DIR/maildir"
else
  mkdir -p "$DATA_DIR/maildir"
fi

echo "Restored into $DATA_DIR"
echo "Previous files kept as *.bak-$STAMP if present."
echo "Start the stack again (docker compose start / ./run.sh start)."
