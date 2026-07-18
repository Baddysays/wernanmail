# Wernanmail server (Phase 2)

Own **full corporate mail** stack in pure Go: SMTP (inbound + submission), IMAP, durable queue, antispam, antivirus adapter, and a graphical admin UI — without Mailcow-class RAM.

Same day-to-day mail ops; calendar/contacts as **optional install profiles**, not mandatory core.

The Phase 1 web client stays a thin IMAP/SMTP client. Point it at this stack when ready — no UI rewrite.

## Goals

- Product aim **≤700 MiB** where practical; host **minimum 1 GiB**, **recommend 2 GiB**
- Core daemons alone typically **~40–150 MiB** (no ClamAV)
- Readable package layout: domain types + interfaces + composition
- Stable API **error codes** for UI translation
- Deployable as light binaries (+ optional Compose) with healthchecks

## Architecture

```
Internet MX:25 ──► smtpd ──► pipeline (antispam → antivirus) ──► queue ──► worker ──► store
Users :587 (or optional :465) ► submission ─────────────────────────┘              │
Users :143 (or optional :993) ► imapd ◄────────────────────────────────────────────┘
Admin UI HTTPS ► admin API ──► store / queue / settings / quarantine
Web client ────► existing BFF (Phase 1) ──► this IMAP/SMTP
```

### Processes

| Binary | Role |
|--------|------|
| `cmd/mta` | SMTP inbound (:25) + authenticated submission (:587) |
| `cmd/imapd` | IMAP (:143; wrap :993 via TLS terminator or native TLS) |
| `cmd/worker` | Queue consumer: local deliver, outbound SMTP, bounce |
| `cmd/admin` | Admin HTTP API (+ optional static admin UI) |
| `cmd/api` | Existing client BFF (Phase 1) |

### Storage

- **SQLite** — domains, mailboxes, aliases, message metadata, queue, settings, audit
- **Maildir** — raw RFC822 bodies on disk under `data/maildir/`

### Packages (`server/internal`)

| Package | Responsibility |
|---------|----------------|
| `domain` | `Domain`, `Mailbox`, `Message`, `QueueJob`, `SpamVerdict`, … |
| `store` | Persistence interfaces + SQLite / Maildir implementations |
| `queue` | Durable jobs, lease, backoff, DLQ |
| `pipeline` | Inbound: spam → AV → enqueue / quarantine |
| `antispam` | Scoring engine (SPF/DKIM/DMARC hooks, RBL, heuristics) |
| `antivirus` | `Scanner` interface; `light`/`noop`; optional ClamAV on larger hosts |
| `dnsauth` | SPF verify, DKIM sign/verify, ARC seal/verify, Authentication-Results |
| `outbound` | MX resolve + SMTP client |
| `smtpd` / `imapd` | Protocol daemons |
| `adminapi` | Admin REST |
| `settings` | Typed settings tree + rate limits / quotas |

## Go-live checklist (operator)

Do **not** commit real hostnames/IPs into the public repo.

### First install (Docker, plain language)

1. Point **A/AAAA** for `mail.example.com` at the VPS **before** you ask for Let's Encrypt  
2. On the VPS: `curl -fsSL https://raw.githubusercontent.com/Baddysays/wernanmail/main/install.sh | bash`  
3. Answer the prompts (hostname, public URL, contact email, TLS yes/no)  
4. Open Admin → follow **Setup — go live** and **DNS helper**  
5. Create a mailbox, send a test, check **Deliverability**

CI / scripts (no prompts):

```bash
WERNANMAIL_NONINTERACTIVE=1 \
  MAIL_HOSTNAME=mail.example.com \
  PUBLIC_URL=https://mail.example.com \
  CERTBOT_EMAIL=you@example.com \
  ./install.sh
```

### DNS

1. Wait until the domain is **delegated** at the TLD (public resolvers must answer)
2. **A/AAAA** — apex (site) + `mail` host
3. **MX** — domain → `mail.…` (priority 10)
4. **SPF** — `v=spf1 mx a:mail.… -all` (or `ip4:` of the outbound IP)
5. **DKIM** — publish public key from admin → Domains → DKIM
6. **DMARC** — start with `v=DMARC1; p=none; rua=mailto:postmaster@…`
7. **PTR** — reverse DNS for the outbound IP (VPS provider) matching `MAIL_EHLO` / `MAIL_HOSTNAME`
8. Optional: **MTA-STS** (`_mta-sts` TXT + HTTPS policy at `https://mta-sts.<domain>/.well-known/mta-sts.txt` — point `mta-sts` A/AAAA at the API host or reverse-proxy `/.well-known/mta-sts.txt`), **TLS-RPT** (`_smtp._tls` TXT; aggregates land in admin like DMARC), **BIMI** (`default._bimi` TXT + SVG at `l=` URL; no VMC required for the helper)
9. Firewall: **25, 587, 143, 80, 443** by default; add **465 / 993** only if you expose implicit TLS

### TLS

Compose starts with a **temporary self-signed** certificate so ports come up on day one.
Browsers will warn until you install a real certificate — that is expected.

Recommended path (HTTP-01 via host Certbot → Docker `mail_tls` volume):

```bash
# After DNS points here and port 80 is open:
./scripts/issue-tls-certbot.sh
# or:
MAIL_HOSTNAME=mail.example.com CERTBOT_EMAIL=you@example.com ./scripts/issue-tls-certbot.sh
```

Manual path:

```bash
certbot certonly --webroot -w /var/www/certbot -d mail.example.com
LIVE_DIR=/etc/letsencrypt/live/mail.example.com ./scripts/issue-tls-certbot.sh
```

DNS-01 is preferred when covering apex + `mail` without opening HTTP on the mail host.
Built-in ACME inside the MTA is not required for v1; host-level Certbot/Caddy is fine.

### Backup / restore

Full message store (`mail.db` + `maildir/`):

```bash
./scripts/backup-data.sh /path/to/data ./wernanmail-backup.tgz
# stop stack first
WERNANMAIL_RESTORE_CONFIRM=yes ./scripts/restore-data.sh ./wernanmail-backup.tgz /path/to/data
```

Daily cron (native install): install `scripts/cron-backup.sh` and the snippet
`deploy/mail-host/cron.d-wernanmail-backup` as `/etc/cron.d/wernanmail-backup`.

```bash
install -m 755 scripts/cron-backup.sh /opt/wernanmail/scripts/cron-backup.sh
install -m 644 deploy/mail-host/cron.d-wernanmail-backup /etc/cron.d/wernanmail-backup
mkdir -p /var/backups/wernanmail
```

Runs at 03:15 UTC into `/var/backups/wernanmail/mail-*.tgz`, keeps 7 days (`KEEP_DAYS`).
Log: `/var/log/wernanmail-backup.log`.

Once a month, restore a copy into a throwaway directory and confirm `mail.db` opens
(`sqlite3 … .tables`) before you need it in anger.

Admin UI:

- **Full backup** — `GET /api/admin/backup/full` streams `mail.db` + `maildir/` as `.tar.gz` (same payload as the script). Restore remains CLI-only via `restore-data.sh`.
- **Config JSON** — `GET /api/admin/backup` exports domains/mailboxes/settings only (no message bodies, passwords, or DKIM private keys).

### Readiness & outbound posture

- `GET /readyz` (no auth) — slim `{status: ok|degraded}` for the public; HTTP 503 when degraded.
  Details (queue/procs) only for loopback or `SCRAPE_ALLOW` CIDRs.
- `GET /api/admin/posture` (auth) — outbound IP, PTR vs `MAIL_EHLO`, DNSBL cleanliness, antispam self-test, stack/queue summary.

Optional overrides:

- `MAIL_PUBLIC_IP=x.x.x.x` when the mail hostname does not resolve to the sending address.
- `SCRAPE_ALLOW=10.0.0.0/8` for Prometheus hosts (loopback always allowed).
- `WERNANMAIL_STACK_CHECK=skip` to disable `/proc` process checks (auto in Docker).

### Metrics

Admin exposes Prometheus text metrics at `GET /metrics` (no auth, **loopback /
`SCRAPE_ALLOW` only**; nginx edge also `allow 127.0.0.1; deny all`):

- queue pending / dead
- quarantine open count
- domains / mailboxes
- host mail RSS + data dir size

Optional per-daemon scrape ports: set `METRICS_ADDR=:9101` on `mta` / `worker`
(and any other process) for process-local counters (`jobs_ok`, `smtp_inbound_accepted`, …).

Worker also emits structured `slog` lines for queue job ok/fail.

SQLite schema is versioned via `schema_migrations` (see `server/internal/store/sqlite/migrate.go`).
Admin ops status exposes `schemaVersion`; Overview links to `/metrics`.

### Smoke after install

1. `docker compose ps` — all services healthy  
2. Admin login → Domains → DNS chips green (or documented pending)  
3. Send mailbox ↔ external MX; confirm HTML + attachment round-trip  
4. Check worker log for `outbound ok` (TLS to remote MX)  
5. Open admin **Deliverability** for DMARC aggregate rows once RUA mail arrives

## RAM budget

| | |
|--|--|
| **Host minimum** | **1 GiB** |
| **Host recommended** | **2 GiB** |
| **Product aim** | stay near **≤700 MiB** total when possible |
| Core daemons (mta + imapd + worker + admin) | ~40–150 MiB observed |
| + webmail | same host or separate |
| + ClamAV / heavy AV | +200–400 MiB — prefer ≥2 GiB hosts |
| + calendar / contacts | install-time options (not always-on) |
| Never co-locate our MTA ports with Mailcow on one public IP |

## Install

### Docker (recommended)

```bash
docker compose up --build -d
```

| What | Where |
|------|-------|
| Admin | `https://your-host/admin/` |
| Webmail | `https://your-host/` |
| SMTP | port `25` |
| SMTP submit | port `587` with STARTTLS |
| IMAP | port `143` with STARTTLS |

The init service generates persistent secrets and a self-signed bootstrap
certificate. Replace the certificate in the `mail_tls` Docker volume before
public use. ClamAV is not started by default; lightweight attachment scanning
is built into the MTA.

### Binaries (small VPS)

1. Cross-compile `admin` `api` `imapd` `mta` `worker` for `linux/amd64`
2. Copy into `/opt/wernanmail/bin/` with `www/` (webmail) and `www/admin/` (SPA)
3. Configure `.env` (see `.env.example`)
4. Start: [`deploy/mail-host/run.sh`](../deploy/mail-host/run.sh) → `./run.sh start`

Compose file: [`docker-compose.yml`](../docker-compose.yml)

## Admin UI

SPA under [`admin/`](../admin/) — domains, mailboxes, queue, quarantine, settings, audit.

**Look:** Overview = quiet console; other screens = operator health strip (MX/SPF/DKIM/DMARC/TLS/Queue). Paper Quiet palette.

Product shots: [`docs/mockups/admin-overview.png`](mockups/admin-overview.png), [`docs/mockups/login-desktop.png`](mockups/login-desktop.png) · full set in [`docs/mockups/`](mockups/)

## Coding rules

- One package ≈ one responsibility; file names match roles (`mailbox.go`, `quarantine.go`)
- Cross-boundary deps via interfaces (`MessageStore`, `Queue`, `SpamEngine`, `VirusScanner`, `MailTransporter`)
- No secrets or private infra inventory in git
