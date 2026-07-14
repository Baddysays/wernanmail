# Wernanmail

Lightweight self-hosted **mail client** (web). Own mail server comes later.

**Live (planned):** https://wernanmail.baddysays.ru  
**Host:** 157.22.202.235

## Product policy

| Principle | Meaning |
|-----------|---------|
| **Light** | Whole product target **~700MB RAM**, recommend **1GB** (not Mailcow-class 6GB+) |
| **Fast** | Keyboard-first UI, snappy inbox, thin Go API |
| **Reliable** | Mailcow-inspired containerization & ops discipline — without Mailcow weight |

Phase 1 = **client** (IMAP/SMTP to existing servers).  
Phase 2 = **server** (own stack, still light).

## Unique feature: **Mailport**

Embeddable mail for other products — iframe / web component / JS SDK.

- Drop inbox or compose into Loccore, Foxik, admin panels, client portals
- Same session / scoped API tokens
- Themes match host product

Self-hosted mail that **plugs into your stack**, not only a standalone webmail.

## Stack (MVP client)

| Layer | Choice |
|-------|--------|
| Frontend | React 19 + Vite + TypeScript |
| Styles | CSS Modules + CSS variables (themes) |
| Backend | Go (chi) + go-imap + SMTP |
| Sessions | httpOnly cookies |
| Deploy | Docker Compose, one (or few) light containers |

## Repo layout

`
web/       # React client
server/    # Go API
docs/      # mockups, architecture
`

## Status

Scaffolding + UI direction. Client first → deploy to wernanmail.baddysays.ru → then server.

---

*by [baddysays](https://github.com/Baddysays) · [hello@baddysays.ru](mailto:hello@baddysays.ru)*
