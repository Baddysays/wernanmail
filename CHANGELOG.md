# Changelog

All notable changes to Wernanmail will be documented in this file.

## [Unreleased]

## [0.4.0] - 2026-07-18

### Added

- Admin deliverability score **0–10** from SPF/DKIM/DMARC/MX/PTR/RBL/antispam (`rating` on `GET /api/admin/posture`).
- Operator **alerts** in Settings: email / Telegram / webhook, watchdog in admin process, test button (`POST /api/admin/alerts/test`).
- **ARC** (RFC 8617): seal outbound with domain DKIM keys after DKIM; verify inbound chain and prepend `Authentication-Results` (spf/dkim/arc).
- **MTA-STS / TLS-RPT / BIMI** posture checks (optional; missing does not lower score), DNS helper chips, TLS-RPT ingest + `GET /api/admin/tls-rpt-reports` (schema v3).

### Fixed

- Concurrent SQLite migrations on multi-process start no longer crash when another process already stamped the same version.

## [0.3.0] - 2026-07-17

### Added

- Prometheus `/metrics` on admin (store gauges) and api; optional `METRICS_ADDR` for mta/worker.
- Structured `slog` lines for queue job ok/fail in the worker.
- Versioned SQLite migrations (`schema_migrations`, current schema v2).
- Admin Overview: DB schema version + link to `/metrics`.
- Compose nginx + docker-smoke checks for Prometheus metrics.
- Public `GET /readyz` for stack/queue readiness (503 when degraded).
- Authenticated `GET /api/admin/posture`: outbound IP cleanliness (PTR + DNSBL), antispam probe, stack status.
- Admin Deliverability: PTR/IP/Spam rows + recheck; HealthStrip STACK/IP/SPAM chips.
- Admin **Download full backup** — streams `mail.db` + Maildir as `.tar.gz` (`GET /api/admin/backup/full`).
- Daily backup cron helper (`scripts/cron-backup.sh`) with 7-day retention.
- Optional `MAIL_PUBLIC_IP` for reputation checks.

### Changed

- DNSBL lookups ignore Spamhaus-style `127.255.255.*` “open resolver” answers (no false listed).
- Operator docs: readiness, posture, backup cron, restore drill.

## [0.2.0] - 2026-07-17

### Added

- Integration smoke test: SMTP → queue → worker → IMAP → API (`server/internal/integration`).
- Full data backup/restore scripts (`scripts/backup-data.sh`, `scripts/restore-data.sh`).
- Certbot → Compose TLS helper (`scripts/issue-tls-certbot.sh`).
- README “who this is for” honesty table.

### Changed

- IMAP IDLE: cross-process `content_rev` signal with ~500 ms poll (was fixed 5 s FolderStats).
- Operator docs: TLS helper, backup path, default ports clarified.

## [0.1.0] - 2026-07-16

### Added

- GitHub Actions CI for Go tests, SPA builds, and Docker Compose smoke.
- MIT license, security policy, and deploy helper documentation.
- One-line installer that clones the repo when launched via `curl | bash`.

### Changed

- README: CI badge, license badge, honest port guidance, roadmap notes.
- Landing page: real product screenshots, OG metadata, showcase section.
- Admin deliverability card: DMARC empty-state hint (RU/EN).

## Unreleased

### Fixed

- Full admin backup packs to a temp archive before streaming (no truncated 200 OK).
- `/metrics` restricted to loopback + `SCRAPE_ALLOW`; nginx edge deny-by-default.
- Public `/readyz` returns slim `{status}`; details only for scrape-allowed clients.
- DNSBL: NXDOMAIN = clean; resolver errors = inconclusive (not false-clean).
- Stack process checks skipped in Docker (`/.dockerenv` / `WERNANMAIL_STACK_CHECK=skip`).
- `/readyz` wired when `ADMIN_UI_DIR` serves the SPA.

### Still open

- Mailport embed surface (preview only).
- Built-in ACME inside the MTA (host Certbot helper remains the supported path).
