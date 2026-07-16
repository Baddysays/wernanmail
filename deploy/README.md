# Deploy helpers

This folder contains operator scripts for **native binary** installs on a Linux
mail host (`/opt/wernanmail`).

## In git

| Path | Purpose |
|------|---------|
| [`mail-host/run.sh`](mail-host/run.sh) | Start/stop `mta`, `imapd`, `worker`, `admin`, `api` |
| [`mail-host/nginx-mail.conf.fragment`](mail-host/nginx-mail.conf.fragment) | nginx body for webmail + admin SPA |
| [`mail-host/apply-user-admin-split.sh`](mail-host/apply-user-admin-split.sh) | Split user webmail and admin API on one host |
| [`mail-host/enable-starttls.sh`](mail-host/enable-starttls.sh) | Enable STARTTLS on native daemons |

## Not in git

Host-specific diagnostics, smoke tests, and one-off agent scripts under
`deploy/mail-host/` are intentionally gitignored. Keep secrets and private
inventory out of the public repository.

## Recommended paths

- **Docker install:** [`install.sh`](../install.sh) at the repo root
- **Native install:** build Linux binaries, copy to `/opt/wernanmail/bin/`,
  deploy `web/dist` → `www/` and `admin/dist` → `www/admin/`, then
  [`mail-host/run.sh start`](mail-host/run.sh)

Operator checklist: [`docs/SERVER.md`](../docs/SERVER.md)
