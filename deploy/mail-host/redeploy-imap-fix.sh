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
tail -n 2 logs/imapd.log

bash /tmp/repair-folder-uid.sh

python3 <<'PY'
import imaplib, ssl
ctx=ssl.create_default_context()
M=imaplib.IMAP4_SSL('mail.wernanmail.ru', 993, ssl_context=ctx)
M.login('baddy@wernanmail.ru','BdDG5KoP7VVZrs9')
print('LIST', M.list())
print('STATUS', M.status('INBOX','(MESSAGES RECENT UIDNEXT UIDVALIDITY UNSEEN)'))
print('SELECT', M.select('INBOX'))
typ, data = M.uid('search', None, 'ALL')
print('UID SEARCH', typ, data)
uid = data[0].split()[0]
# Outlook-like fetch
typ, msg = M.uid('fetch', uid, '(UID FLAGS INTERNALDATE RFC822.SIZE BODYSTRUCTURE BODY.PEEK[])')
print('FETCH typ', typ)
if msg and isinstance(msg[0], tuple):
    print('meta', msg[0][0][:200] if isinstance(msg[0][0], (bytes,bytearray)) else msg[0][0])
    print('body_len', len(msg[0][1]))
    print('has Re:', b'Re:' in msg[0][1] or b'Subject' in msg[0][1])
else:
    print('msg', msg)
# also Sent
print('SELECT Sent', M.select('Sent'))
print('UID SEARCH Sent', M.uid('search', None, 'ALL'))
M.logout()
print('OK')
PY
