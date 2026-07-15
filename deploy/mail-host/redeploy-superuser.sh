#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail

for p in mta imapd worker admin api; do
  if [ -f "logs/$p.pid" ]; then kill "$(cat logs/$p.pid)" 2>/dev/null || true; rm -f "logs/$p.pid"; fi
done
pkill -f '/opt/wernanmail/bin/(mta|imapd|worker|admin|api)' 2>/dev/null || true
sleep 1

install -m 755 /tmp/wm-deploy/bin/admin /opt/wernanmail/bin/admin
install -m 755 /tmp/wm-deploy/bin/mta /opt/wernanmail/bin/mta
install -m 755 /tmp/wm-deploy/bin/imapd /opt/wernanmail/bin/imapd
install -m 755 /tmp/wm-deploy/bin/api /opt/wernanmail/bin/api
install -m 755 /tmp/wm-deploy/bin/worker /opt/wernanmail/bin/worker

rm -rf /opt/wernanmail/www/admin/*
mkdir -p /opt/wernanmail/www/admin
cp -a /tmp/wm-deploy/admin/. /opt/wernanmail/www/admin/
cp -a /tmp/wm-deploy/web/. /opt/wernanmail/www/
chmod -R a+rX /opt/wernanmail/www

if ! grep -q '^SESSION_SECRET=' .env; then
  echo "SESSION_SECRET=$(openssl rand -hex 32)" >> .env
  echo "ADDED_SESSION_SECRET"
fi
if ! grep -q '^MAIL_MASTER_PASSWORD=' .env; then
  echo "MAIL_MASTER_PASSWORD=$(openssl rand -hex 24)" >> .env
  echo "ADDED_MAIL_MASTER_PASSWORD"
fi
if ! grep -q '^WEBMAIL_URL=' .env; then
  echo "WEBMAIL_URL=https://mail.wernanmail.ru" >> .env
  echo "ADDED_WEBMAIL_URL"
fi

set -a
# shellcheck disable=SC1091
. ./.env
set +a
export DATA_DIR=/opt/wernanmail/data
export MAIL_HOSTNAME="${MAIL_HOSTNAME:-mail.wernanmail.ru}"

for p in mta imapd worker admin api; do
  nohup ./bin/$p >>logs/$p.log 2>&1 &
  echo $! >logs/$p.pid
  echo "started $p pid=$(cat logs/$p.pid)"
done
sleep 2

echo "--- verify ---"
curl -sS http://127.0.0.1:8090/healthz; echo
curl -sS http://127.0.0.1:8080/healthz; echo
grep -oE "openAsUser|superuser_enabled|defaultQuotaMb" /opt/wernanmail/www/admin/assets/*.js | head -10 || true
grep -oE "impersonat[a-zA-Z]*" /opt/wernanmail/www/assets/*.js | sort -u | head -10 || true
strings /opt/wernanmail/bin/admin | grep -E "superuser_enabled|impersonateMailbox|webmail_url" | head -10
ss -tlnp | grep -E ":8090|:8080|:143|:2587" || true
echo DONE
