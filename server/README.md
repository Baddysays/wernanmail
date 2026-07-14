# Wernanmail API (Go)

Thin IMAP/SMTP client backend for the `web/` React app.

## Run

```bash
cd server
cp .env.example .env   # optional
go run ./cmd/api
```

Listens on `:8080` by default (`ADDR` or `PORT`). Vite should proxy `/api` → `http://localhost:8080`.

```bash
go build -o bin/wernanmail-api ./cmd/api
```

## Endpoints

| Method | Path | Auth | Notes |
|--------|------|------|--------|
| `GET` | `/api/health` | no | Liveness |
| `POST` | `/api/auth/login` | no | Body: `imapHost`, `imapPort`, `smtpHost`, `smtpPort`, `username`, `password`, `tls?` → sets httpOnly `wernan_sid` cookie |
| `POST` | `/api/auth/logout` | cookie | Clears session |
| `GET` | `/api/folders` | cookie | IMAP mailbox list (`/api/mailboxes` alias) |
| `GET` | `/api/messages?folder=INBOX&limit=50` | cookie | Message summaries |
| `GET` | `/api/messages/{id}?folder=INBOX` | cookie | Full message (`id` = IMAP UID) |
| `POST` | `/api/messages/send` | cookie | Body: `to[]`, `cc?`, `bcc?`, `subject`, `text`, `html?` |

Errors are machine codes only, e.g. `{"code":"mail.auth_failed"}` — the UI translates.

## Sessions

In-memory map keyed by session id (MVP). Mailbox password stays on the server in the session store; never in frontend `localStorage`.

Later: SQLite-backed session store.

## CORS

`CORS_ORIGINS` defaults to Vite localhost origins with `AllowCredentials: true`.
