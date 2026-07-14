# Wernanmail server (Phase 2)

Own light mail stack in pure Go: SMTP (inbound + submission), IMAP, durable queue, antispam, antivirus adapter, and a graphical admin UI.

The Phase 1 web client stays a thin IMAP/SMTP client. Point it at this stack when ready — no UI rewrite.

## Goals

- Fit roughly **150–250 MiB** RAM for core services (no ClamAV)
- Readable package layout: domain types + interfaces + composition
- Stable API **error codes** for UI translation
- Containerized with healthchecks and hard `mem_limit`s

## Architecture

```
Internet MX:25 ──► smtpd ──► pipeline (antispam → antivirus) ──► queue ──► worker ──► store
Users :587 ────► submission ──────────────────────────────────────┘              │
Users :993 ────► imapd ◄──────────────────────────────────────────────────────────┘
Admin UI ──────► admin API ──► store / queue / settings / quarantine
Web client ────► existing BFF (Phase 1) ──► this IMAP/SMTP
```

### Processes

| Binary | Role |
|--------|------|
| `cmd/mta` | SMTP inbound (:25) + authenticated submission (:587) |
| `cmd/imapd` | IMAP (:143 / :993) over the message store |
| `cmd/worker` | Queue consumer: local deliver, outbound SMTP, bounce |
| `cmd/admin` | Admin HTTP API |
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
| `antivirus` | `Scanner` interface; `noop` default; optional ClamAV |
| `dnsauth` | SPF verify, DKIM sign/verify, DMARC |
| `outbound` | MX resolve + SMTP client |
| `smtpd` / `imapd` | Protocol daemons |
| `adminapi` | Admin REST |
| `settings` | Typed settings tree + rate limits / quotas |

## DNS checklist (operator)

Do **not** commit real hostnames/IPs into the public repo. On your DNS provider:

1. **A/AAAA** — mail host
2. **MX** — domain → mail host (priority 10)
3. **SPF** — `v=spf1 mx -all` (adjust for relays)
4. **DKIM** — publish public key from admin → Domains → DKIM
5. **DMARC** — start with `v=DMARC1; p=none; rua=mailto:postmaster@…`
6. **PTR** — reverse DNS for the outbound IP (ask the VPS provider)
7. Open firewall: **25, 465, 587, 993** (and admin HTTPS via reverse proxy)

## RAM budget

| Mode | Approx |
|------|--------|
| Core (mta + imapd + worker + admin + web) | 150–250 MiB |
| + ClamAV profile `av` | +200–400 MiB — use only on ≥2 GiB hosts |
| Never co-locate with Mailcow-class stacks in one compose profile |

## Compose

See [`docker-compose.mail.yml`](../docker-compose.mail.yml). Default data dir: `./data` (gitignored).

```bash
docker compose -f docker-compose.mail.yml up --build -d
```

Optional antivirus:

```bash
docker compose -f docker-compose.mail.yml --profile av up -d
```

## Admin UI

SPA under [`admin/`](../admin/) — domains, mailboxes, queue, quarantine (spam score breakdown), settings (limits, relay, TLS paths, antispam thresholds), audit log.

Default admin bootstrap: set `ADMIN_USER` / `ADMIN_PASSWORD` in env (see `.env.mail.example`).

## Coding rules

- One package ≈ one responsibility; file names match roles (`mailbox.go`, `quarantine.go`)
- Cross-boundary deps via interfaces (`MessageStore`, `Queue`, `SpamEngine`, `VirusScanner`, `MailTransporter`)
- No secrets or private infra inventory in git
