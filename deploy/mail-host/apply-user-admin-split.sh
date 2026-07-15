#!/usr/bin/env bash
set -euo pipefail
chmod +x /opt/wernanmail/bin/api /opt/wernanmail/run.sh

if ! grep -q '^COOKIE_SECURE=' /opt/wernanmail/.env 2>/dev/null; then
  printf '\nADDR=:8080\nCOOKIE_SECURE=true\nCORS_ORIGINS=https://mail.wernanmail.ru\n' >> /opt/wernanmail/.env
fi
sed -i '/^ADMIN_UI_DIR=/d' /opt/wernanmail/.env || true

cat > /etc/nginx/sites-available/mail.wernanmail.ru <<'NGINX'
server {
  listen 80;
  server_name mail.wernanmail.ru;
  location ^~ /.well-known/acme-challenge/ {
    root /var/www/html;
  }
  location / {
    return 301 https://$host$request_uri;
  }
}

server {
  listen 443 ssl;
  server_name mail.wernanmail.ru;
  ssl_certificate /etc/letsencrypt/live/mail.wernanmail.ru/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/mail.wernanmail.ru/privkey.pem;
  ssl_protocols TLSv1.2 TLSv1.3;

  root /opt/wernanmail/www;
  index index.html;

  location = /admin {
    return 301 /admin/;
  }

  location /admin/ {
    try_files $uri $uri/ /admin/index.html;
  }

  location /api/admin/ {
    proxy_pass http://127.0.0.1:8090;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Authorization $http_authorization;
  }

  location = /healthz {
    proxy_pass http://127.0.0.1:8090/healthz;
  }

  location /api/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }

  location / {
    try_files $uri $uri/ /index.html;
  }
}
NGINX

ln -sfn /etc/nginx/sites-available/mail.wernanmail.ru /etc/nginx/sites-enabled/mail.wernanmail.ru
nginx -t
systemctl reload nginx

set -a
# shellcheck disable=SC1091
. /opt/wernanmail/.env
set +a
unset ADMIN_UI_DIR

for p in admin api; do
  if [ -f "/opt/wernanmail/logs/$p.pid" ]; then
    kill "$(cat /opt/wernanmail/logs/$p.pid)" 2>/dev/null || true
    rm -f "/opt/wernanmail/logs/$p.pid"
  fi
done
# also kill orphan listeners on 8080/8090 if needed
pkill -f '/opt/wernanmail/bin/admin' 2>/dev/null || true
pkill -f '/opt/wernanmail/bin/api' 2>/dev/null || true
sleep 1

nohup /opt/wernanmail/bin/admin >>/opt/wernanmail/logs/admin.log 2>&1 & echo $! >/opt/wernanmail/logs/admin.pid
nohup /opt/wernanmail/bin/api >>/opt/wernanmail/logs/api.log 2>&1 & echo $! >/opt/wernanmail/logs/api.pid
sleep 1

echo "--- listeners ---"
ss -tlnp | grep -E ':8080|:8090|:443' || true
echo "--- / ---"
curl -skI https://127.0.0.1/ | head -8
echo "--- /admin/ ---"
curl -skI https://127.0.0.1/admin/ | head -8
echo "--- titles ---"
curl -sk https://127.0.0.1/ | tr '\n' ' ' | grep -o '<title>[^<]*</title>' || true
curl -sk https://127.0.0.1/admin/ | tr '\n' ' ' | grep -o '<title>[^<]*</title>' || true
echo "--- api ---"
tail -3 /opt/wernanmail/logs/api.log || true
curl -skI https://127.0.0.1/api/auth/login | head -5 || true
