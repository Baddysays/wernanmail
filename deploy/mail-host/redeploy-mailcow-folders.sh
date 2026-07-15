#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail
install -m 755 /tmp/wm-imapd bin/imapd
if [ -f logs/imapd.pid ]; then kill "$(cat logs/imapd.pid)" 2>/dev/null || true; fi
pkill -f '/opt/wernanmail/bin/imapd' 2>/dev/null || true
sleep 1
set -a; . ./.env; set +a
nohup ./bin/imapd >>logs/imapd.log 2>&1 & echo $! >logs/imapd.pid
sleep 1
python3 <<'PY'
import imaplib, ssl
c = ssl.create_default_context()
M = imaplib.IMAP4_SSL("mail.wernanmail.ru", 993, ssl_context=c)
M.login("baddy@wernanmail.ru", "BdDG5KoP7VVZrs9")
print("LIST:")
for d in M.list()[1]:
    print(" ", d)
print("Sent", M.select("Sent"), M.uid("search", None, "ALL"))
print("Sent Items", M.select("Sent Items"), M.uid("search", None, "ALL"))
M.logout()
PY
