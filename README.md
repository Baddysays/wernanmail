# Wernanmail

Lightweight self-hosted **mail client** (web). Own mail server comes later.

## Product policy

| Principle | Meaning |
|-----------|---------|
| **Light** | Whole product target **~700MB RAM**, recommend **1GB** (not Mailcow-class 6GB+) |
| **Fast** | Keyboard-first UI, snappy inbox, thin Go API |
| **Reliable** | Mailcow-inspired containerization and ops discipline — without Mailcow weight |

Phase 1 = **client** (IMAP/SMTP to existing servers).  
Phase 2 = **server** (own stack, still light).

## Unique feature: **Mailport**

Embeddable mail for other products — iframe / web component / JS SDK.

- Drop inbox or compose into other apps and admin panels
- Same session / scoped API tokens
- Themes match host product

Self-hosted mail that **plugs into your stack**, not only a standalone webmail.

## Design direction

Default look: **Paper Quiet** — light, calm, readable, three-column mail UI.

In **Settings** (first-class, not an afterthought):

- **Font** — user-selectable typefaces for UI / reading
- **Color** — accent palette with **several gradations** (not a single hard-coded teal)
- **Theme** — light / dark (and density later)

Everything in the product is **multilingual** (i18n from day one: UI strings, settings, errors — no hard-coded single language).

See [docs/DESIGN.md](docs/DESIGN.md) and mockups in docs/mockups/.

## Stack (MVP client)

| Layer | Choice |
|-------|--------|
| Frontend | React 19 + Vite + TypeScript |
| i18n | react-i18next (or equivalent), locale files |
| Styles | CSS Modules + CSS variables (fonts + color scales) |
| Backend | Go (chi) + go-imap + SMTP |
| Sessions | httpOnly cookies |
| Deploy | Docker Compose, light containers |

## Repo layout

`
web/       # React client
server/    # Go API
docs/      # design, policy, mockups
`

## Status

Design locked toward Paper Quiet + customizable fonts/colors + full i18n. Client first, server later.

---

*by [baddysays](https://github.com/Baddysays)*
