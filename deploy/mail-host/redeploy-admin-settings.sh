#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail
if [ -f /tmp/wm-admin ]; then
  install -m 755 /tmp/wm-admin /opt/wernanmail/bin/admin
fi
chmod -R a+rX /opt/wernanmail/www/admin
if [ -f logs/admin.pid ]; then
  kill "$(cat logs/admin.pid)" 2>/dev/null || true
  rm -f logs/admin.pid
fi
# avoid killing unrelated processes named admin
pkill -f '/opt/wernanmail/bin/admin' 2>/dev/null || true
sleep 1
set -a
# shellcheck disable=SC1091
. ./.env
set +a
unset ADMIN_UI_DIR
nohup ./bin/admin >>logs/admin.log 2>&1 &
echo $! >logs/admin.pid
sleep 1
echo "PID=$(cat logs/admin.pid)"
ss -tlnp | grep 8090 || true
ls -la www/admin/assets | tail -5
curl -sk https://127.0.0.1/admin/ | grep -oE 'index-[A-Za-z0-9._-]+' | head -5
PASS=$(grep '^ADMIN_PASSWORD=' .env | cut -d= -f2-)
USER=$(grep '^ADMIN_USER=' .env | cut -d= -f2-)
curl -s -u "${USER}:${PASS}" http://127.0.0.1:8090/api/admin/settings | python3 -c 'import sys,json; d=json.load(sys.stdin)["data"]; print("min_len", d.get("security.password_min_length")); print("flag_at", d.get("antispam.flag_at")); print("qret", d.get("quarantine.retention_days")); print("has_footer", "mail.footer_plain" in d)'
