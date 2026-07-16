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
| `dnsauth` | SPF verify, DKIM sign/verify, DMARC |
| `outbound` | MX resolve + SMTP client |
| `smtpd` / `imapd` | Protocol daemons |
| `adminapi` | Admin REST |
| `settings` | Typed settings tree + rate limits / quotas |

## Go-live checklist (operator)

Do **not** commit real hostnames/IPs into the public repo.

### DNS

1. Wait until the domain is **delegated** at the TLD (public resolvers must answer)
2. **A/AAAA** — apex (site) + `mail` host
3. **MX** — domain → `mail.…` (priority 10)
4. **SPF** — `v=spf1 mx a:mail.… -all` (or `ip4:` of the outbound IP)
5. **DKIM** — publish public key from admin → Domains → DKIM
6. **DMARC** — start with `v=DMARC1; p=none; rua=mailto:postmaster@…`
7. **PTR** — reverse DNS for the outbound IP (VPS provider) matching `MAIL_HOSTNAME`
8. Optional: **MTA-STS** (`_mta-sts` TXT + `mta-sts.` HTTPS policy) and **TLS-RPT**
9. Firewall: **25, 587, 143** + HTTPS **443** by default; add **465 / 993** only if you expose implicit TLS

### TLS

Compose starts with a **self-signed** bootstrap cert so ports come up on day one.
For production, replace files in the `mail_tls` volume as `fullchain.pem` + `privkey.pem`.

Helper (HTTP-01 via host Certbot → install into Compose volume):

```bash
MAIL_HOSTNAME=mail.example.com ./scripts/issue-tls-certbot.sh
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

Admin UI also exports **directory metadata** (domains/mailboxes/settings) via
`GET /api/admin/backup` — that path does **not** include message bodies.

### Metrics

Admin exposes Prometheus text metrics at `GET /metrics` (no auth — scrape only from
a private network or localhost):

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
