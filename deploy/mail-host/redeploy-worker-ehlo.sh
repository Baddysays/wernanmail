#!/usr/bin/env bash
set -euo pipefail
install -m 755 /tmp/worker /opt/wernanmail/bin/worker
python3 - <<'PY'
import sqlite3
c = sqlite3.connect("/opt/wernanmail/data/mail.db")
print("del", c.execute("delete from queue_jobs").rowcount)
c.commit()
PY
kill "$(cat /opt/wernanmail/logs/worker.pid)" 2>/dev/null || true
sleep 1
cd /opt/wernanmail
set -a
# shellcheck disable=SC1091
. ./.env
set +a
export MAIL_EHLO=wernanmail.ru
echo "env MAIL_EHLO=$MAIL_EHLO"
nohup ./bin/worker >>logs/worker.log 2>&1 &
echo $! > logs/worker.pid
sleep 1
python3 - <<'PY'
import smtplib
import time
from email.message import EmailMessage

USER = "postmaster@wernanmail.ru"
PASS = "ChangeMe-Postmaster1"
TO = "test-uuo3v38tf@srv1.mail-tester.com"
msg = EmailMessage()
msg["From"] = USER
msg["To"] = TO
msg["Subject"] = "Wernanmail ehlo log probe"
msg.set_content("ehlo debug\n")
with smtplib.SMTP("127.0.0.1", 587, timeout=30) as s:
    s.login(USER, PASS)
    s.send_message(msg)
print("sent")
time.sleep(5)
PY
tail -n 20 /opt/wernanmail/logs/worker.log
