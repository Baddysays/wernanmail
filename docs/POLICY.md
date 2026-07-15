# Wernanmail policy

## Goals
1. **Full corporate mail, low RAM** — complete day-to-day mail ops without Mailcow-class resource cost.
2. **RAM targets:** aim **≤700 MiB** for the running product where practical; document host **minimum 1 GiB**, **recommend 2 GiB** (headroom for optional modules / AV).
3. Speed — UI and API feel instant; keyboard-driven.
4. Reliability — healthchecks, restart, volumes, backups.

## Product shape
- **Primary install:** mail server + webmail (+ admin) as one product via **one Docker command** (`./install.sh` or `docker compose up --build -d`).
- **Second wedge:** **Mailport** — embeddable inbox/compose for other apps on the same core.
- **Optional at install:** calendar, contacts (and heavy AV on larger hosts) — not forced into the hot path.
- **Default AV:** lightweight attachment policy (no ClamAV) so 1–2 GiB hosts stay viable.

## Phases
1. **Client** — webmail against existing IMAP/SMTP (also works against our server).
2. **Server** — own Go MTA/IMAP stack; optional modules follow.

Do not publish private infra details (hosts, IPs, staging URLs) in the public repo.

## Design and localization
- Default visual: Paper Quiet
- Settings: font choice, accent color with multiple gradations, light/dark
- **i18n:** 12 locales from day one — en, ru, de, fr, es, pt, zh, ja, ko, it, pl, tr
- API returns stable **error codes**; the UI translates messages

## Non-goals (core always-on)
- Shipping SOGo-class groupware **inside the mandatory core** (calendar/contacts are **install options**)
- Always-on ClamAV + heavy antispam on 1 GiB hosts
- Heavy AI in the hot path
- Publishing deployment secrets or server inventory in git
- RTL locales (e.g. Arabic) in v1
- Co-locating our MTA on the same public ports as an existing Mailcow instance
