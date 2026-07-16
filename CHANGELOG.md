# Changelog

All notable changes to Wernanmail will be documented in this file.

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

### Added

- Prometheus `/metrics` on admin (store gauges) and api; optional `METRICS_ADDR` for mta/worker.
- Structured `slog` lines for queue job ok/fail in the worker.
- Versioned SQLite migrations (`schema_migrations`, current schema v2).
- Admin Overview: DB schema version + link to `/metrics`.
- Compose nginx + docker-smoke checks for Prometheus metrics.

### Still open

- Mailport embed surface (preview only).
- Built-in ACME inside the MTA (host Certbot helper remains the supported path).

