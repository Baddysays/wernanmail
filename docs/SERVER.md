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
Users :587/465 ► submission ───────────────────────────────────────┘              │
Users :143/993 ► imapd ◄──────────────────────────────────────────────────────────┘
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

## DNS checklist (operator)

Do **not** commit real hostnames/IPs into the public repo. On your DNS provider:

1. Wait until the domain is **delegated** at the TLD (public resolvers must answer)
2. **A/AAAA** — apex (site) + `mail` host
3. **MX** — domain → `mail.…` (priority 10)
4. **SPF** — `v=spf1 mx a:mail.… -all`
5. **DKIM** — publish public key from admin → Domains → DKIM
6. **DMARC** — start with `v=DMARC1; p=none; rua=mailto:postmaster@…`
7. **PTR** — reverse DNS for the outbound IP (VPS provider)
8. Firewall: **25, 465, 587, 993** + admin **443**
9. Prefer TLS cert covering **apex + `*.domain`** (DNS-01) once delegation works

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
cp .env.mail.example .env.mail
docker compose -f docker-compose.mail.yml --env-file .env.mail up --build -d
```

| What | Where |
|------|-------|
| Admin | http://127.0.0.1:3090 |
| Webmail | point Phase 1 client at this IMAP/SMTP, or use Compose web |
| SMTP submit | host `2587` → container `:587` |
| IMAP | host `2143` → container `:143` |

Antivirus (optional, ≥2 GiB RAM):

```bash
docker compose -f docker-compose.mail.yml --env-file .env.mail --profile av up -d
```

### Binaries (small VPS)

1. Cross-compile `admin` `api` `imapd` `mta` `worker` for `linux/amd64`
2. Copy into `/opt/wernanmail/bin/` with `www/` (webmail) and `www/admin/` (SPA)
3. Configure `.env` (see `.env.mail.example`)
4. Start: [`deploy/mail-host/run.sh`](../deploy/mail-host/run.sh) → `./run.sh start`

Compose file: [`docker-compose.mail.yml`](../docker-compose.mail.yml)

## Admin UI

SPA under [`admin/`](../admin/) — domains, mailboxes, queue, quarantine, settings, audit.

**Look:** Overview = quiet console; other screens = operator health strip (MX/SPF/DKIM/DMARC/TLS/Queue). Paper Quiet palette.

Mockups: [`docs/mockups/admin-variant-c-quiet-console.png`](mockups/admin-variant-c-quiet-console.png), [`docs/mockups/admin-variant-b-operator-strip.png`](mockups/admin-variant-b-operator-strip.png)

## Coding rules

- One package ≈ one responsibility; file names match roles (`mailbox.go`, `quarantine.go`)
- Cross-boundary deps via interfaces (`MessageStore`, `Queue`, `SpamEngine`, `VirusScanner`, `MailTransporter`)
- No secrets or private infra inventory in git
