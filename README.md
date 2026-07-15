# Wernanmail

Self-hosted mail that stays light: **webmail + Go mail server + admin** — without Mailcow-class RAM.

**Target:** ~700 MB running · **host:** 1 GB min / 2 GB recommended

<p align="center">
  <img src="docs/mockups/login-moods.png" alt="Wernanmail sign-in" width="780" />
</p>

<p align="center">
  <img src="docs/mockups/admin-variant-c-quiet-console.png" alt="Wernanmail admin overview" width="780" />
</p>

## What you get

| Piece | Role |
|-------|------|
| **Webmail** | React inbox — folders, compose, moods, 12 languages |
| **Mail server** | SMTP · IMAP · queue · antispam · quarantine |
| **Admin** | Domains, mailboxes, DNS helper, settings, audit |
| **Mailport** | Embed inbox/compose in other products (iframe / SDK) |

## Install (simple)

### 1) Webmail only (use an existing IMAP/SMTP server)

```bash
cp .env.example .env
docker compose up --build -d
```

Open **http://localhost:3080**

### 2) Full stack (own mail server + webmail + admin)

```bash
cp .env.mail.example .env.mail
docker compose -f docker-compose.mail.yml --env-file .env.mail up --build -d
```

| Service | URL / port |
|---------|------------|
| Webmail | http://localhost:3080 |
| Admin | http://localhost:3090 |
| SMTP submit | `localhost:2587` → `:587` |
| IMAP | `localhost:2143` → `:143` |

Optional antivirus (needs ≥2 GB RAM):

```bash
docker compose -f docker-compose.mail.yml --env-file .env.mail --profile av up -d
```

### 3) Bare metal (light binaries)

```bash
# build linux amd64 binaries, then on the host:
cd /opt/wernanmail
./run.sh start
```

See [docs/SERVER.md](docs/SERVER.md) for DNS (MX / SPF / DKIM / DMARC), TLS, and ops.

## Dev (without Docker)

```bash
# API
cd server && cp .env.example .env && go run ./cmd/api

# Webmail
pnpm --dir web install && pnpm --dir web dev

# Admin
npm --prefix admin install && npm --prefix admin run dev
```

## Design

**Paper Quiet** — calm teal, three-column mail, soft motion.

Admin direction: quiet overview console + operator health strip on working screens.

More shots: [`docs/mockups/`](docs/mockups/) · notes: [docs/DESIGN.md](docs/DESIGN.md)

<p align="center">
  <img src="docs/mockups/admin-variant-b-operator-strip.png" alt="Admin with operator health strip" width="780" />
</p>

## Repo

```
web/                 webmail (React + TypeScript)
admin/               operator console (React + TypeScript)
server/              Go API + MTA + IMAP + worker + admin API
deploy/mail-host/    binary host helpers
docs/                design, policy, server, mockups
```

## Policy

Light · fast · reliable. No secrets or private infra in git.  
Details: [docs/POLICY.md](docs/POLICY.md)

---

*by [baddysays](https://github.com/Baddysays)*
