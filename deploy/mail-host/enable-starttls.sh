#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail

CERT=/etc/letsencrypt/live/mail.wernanmail.ru/fullchain.pem
KEY=/etc/letsencrypt/live/mail.wernanmail.ru/privkey.pem
test -r "$CERT"
test -r "$KEY"

# upsert TLS env
grep -q '^MAIL_TLS_CERT=' .env && sed -i "s|^MAIL_TLS_CERT=.*|MAIL_TLS_CERT=$CERT|" .env || echo "MAIL_TLS_CERT=$CERT" >> .env
grep -q '^MAIL_TLS_KEY=' .env && sed -i "s|^MAIL_TLS_KEY=.*|MAIL_TLS_KEY=$KEY|" .env || echo "MAIL_TLS_KEY=$KEY" >> .env

restart() {
  local name="$1"
  if [ -f "logs/${name}.pid" ]; then
    kill "$(cat "logs/${name}.pid")" 2>/dev/null || true
    rm -f "logs/${name}.pid"
  fi
  pkill -f "/opt/wernanmail/bin/${name}" 2>/dev/null || true
}

restart mta
restart imapd
sleep 1

set -a
# shellcheck disable=SC1091
. ./.env
set +a

nohup ./bin/mta >>logs/mta.log 2>&1 & echo $! >logs/mta.pid
nohup ./bin/imapd >>logs/imapd.log 2>&1 & echo $! >logs/imapd.pid
sleep 1

echo "mta=$(cat logs/mta.pid) imapd=$(cat logs/imapd.pid)"
tail -n 5 logs/mta.log
tail -n 5 logs/imapd.log

python3 - <<'PY'
import socket
s=socket.create_connection(('127.0.0.1',587),5)
print('banner:', s.recv(1024).decode('utf-8','replace').strip())
s.send(b'EHLO outlook.test\r\n')
buf=b''
while True:
    chunk=s.recv(4096)
    if not chunk:
        break
    buf+=chunk
    lines=buf.split(b'\r\n')
    # final EHLO line is "250 <text>" without hyphen after 250
    if any(line.startswith(b'250 ') for line in lines if line):
        break
text=buf.decode('utf-8','replace')
print(text)
print('HAS_STARTTLS', 'STARTTLS' in text.upper())
s.send(b'QUIT\r\n')
s.close()
PY
