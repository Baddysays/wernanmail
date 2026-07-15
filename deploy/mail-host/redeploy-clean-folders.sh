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
# bump uidvalidity so Outlook drops stale folder map
python3 <<'PY'
import sqlite3, time
c=sqlite3.connect('data/mail.db')
now=int(time.time())
c.execute('update folder_uid set uid_validity=?',(now,))
c.commit()
print('uv bumped', now)
PY
python3 <<'PY'
import imaplib, ssl
c=ssl.create_default_context()
M=imaplib.IMAP4_SSL('mail.wernanmail.ru',993,ssl_context=c)
M.login('baddy@wernanmail.ru','BdDG5KoP7VVZrs9')
print('LIST (should be clean Mailcow-like):')
for d in M.list()[1]:
    print(' ', d)
M.logout()
PY
