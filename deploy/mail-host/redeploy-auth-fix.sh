#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail
[ -f /tmp/wm-mta ] && install -m 755 /tmp/wm-mta bin/mta
[ -f /tmp/wm-imapd ] && install -m 755 /tmp/wm-imapd bin/imapd

for name in mta imapd; do
  if [ -f "logs/${name}.pid" ]; then
    kill "$(cat "logs/${name}.pid")" 2>/dev/null || true
    rm -f "logs/${name}.pid"
  fi
  pkill -f "/opt/wernanmail/bin/${name}" 2>/dev/null || true
done
sleep 1
set -a
. ./.env
set +a
nohup ./bin/mta >>logs/mta.log 2>&1 & echo $! >logs/mta.pid
nohup ./bin/imapd >>logs/imapd.log 2>&1 & echo $! >logs/imapd.pid
sleep 1
tail -n 2 logs/mta.log
tail -n 2 logs/imapd.log
