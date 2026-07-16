#!/usr/bin/env bash
# Full data backup: SQLite + Maildir (messages).
# Usage:
#   ./scripts/backup-data.sh [DATA_DIR] [OUT.tar.gz]
# Env:
#   DATA_DIR  default ./data (Compose volume mount or /opt/wernanmail/data)
set -euo pipefail

DATA_DIR="${1:-${DATA_DIR:-./data}}"
OUT="${2:-./wernanmail-backup-$(date -u +%Y%m%d-%H%M%S).tar.gz}"

if [ ! -d "$DATA_DIR" ]; then
  echo "DATA_DIR not found: $DATA_DIR" >&2
  exit 1
fi
if [ ! -f "$DATA_DIR/mail.db" ]; then
  echo "mail.db missing under $DATA_DIR" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Prefer a consistent SQLite snapshot when sqlite3 is available.
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 "$DATA_DIR/mail.db" "PRAGMA wal_checkpoint(TRUNCATE);"
  sqlite3 "$DATA_DIR/mail.db" ".backup '$TMP/mail.db'"
else
  echo "sqlite3 not found — copying mail.db live (stop mta/worker/imapd for safest snapshot)." >&2
  cp -f "$DATA_DIR/mail.db" "$TMP/mail.db"
  [ -f "$DATA_DIR/mail.db-wal" ] && cp -f "$DATA_DIR/mail.db-wal" "$TMP/mail.db-wal" || true
  [ -f "$DATA_DIR/mail.db-shm" ] && cp -f "$DATA_DIR/mail.db-shm" "$TMP/mail.db-shm" || true
fi

if [ -d "$DATA_DIR/maildir" ]; then
  cp -a "$DATA_DIR/maildir" "$TMP/maildir"
else
  mkdir -p "$TMP/maildir"
fi

mkdir -p "$(dirname "$OUT")"
tar -C "$TMP" -czf "$OUT" .
echo "Backup written: $OUT"
ls -lh "$OUT"
