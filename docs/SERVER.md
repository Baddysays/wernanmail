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

## Compose / binary deploy

- Compose: [`docker-compose.mail.yml`](../docker-compose.mail.yml)
- Light binary deploy: [`deploy/mail-host/run.sh`](../deploy/mail-host/run.sh)

```bash
docker compose -f docker-compose.mail.yml up --build -d
# or: cross-compile linux amd64 binaries and use deploy/mail-host/run.sh
```

Optional antivirus:

```bash
docker compose -f docker-compose.mail.yml --profile av up -d
```

## Admin UI

SPA under [`admin/`](../admin/) — domains, mailboxes, queue, quarantine (spam score), settings, audit.

**Target look (locked):** **C on Overview, B everywhere else**
- **Overview (C):** calm “Mail is healthy”, queue sparkline, quarantine count, DNS helper slide-over with copyable SPF/DKIM/DMARC
- **Working screens (B):** always-visible Operator health strip (MX/SPF/DKIM/DMARC/TLS/Queue); top nav; master–detail for Domains / Mailboxes / Queue / Quarantine / Settings (list + side panel for aliases, roles, quotas)
- Paper Quiet palette (teal/ink), shared with the webmail client where practical

Visual direction: Paper Quiet — see `docs/mockups/wernanmail-style-02-paper-quiet.png` and `docs/DESIGN.md`.

## Coding rules

- One package ≈ one responsibility; file names match roles (`mailbox.go`, `quarantine.go`)
- Cross-boundary deps via interfaces (`MessageStore`, `Queue`, `SpamEngine`, `VirusScanner`, `MailTransporter`)
- No secrets or private infra inventory in git
