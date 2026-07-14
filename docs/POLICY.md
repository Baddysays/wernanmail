# Wernanmail policy

## Goals
1. Lightness — fit in ~700MB RAM; recommend 1GB for the full product.
2. Speed — UI and API feel instant; keyboard-driven.
3. Reliability — containerized like Mailcow (healthchecks, restart, volumes), without Mailcow resource cost.

## Phases
1. **Client** — webmail against existing IMAP/SMTP.
2. **Server** — own MTA/IMAP stack, still light; integrations follow.

Do not publish private infra details (hosts, IPs, staging URLs) in the public repo.

## Unique wedge
**Mailport** — embeddable mail surfaces (SDK / iframe) for other products.

## Design and localization
- Default visual: Paper Quiet
- Settings: font choice, accent color with multiple gradations, light/dark
- **i18n:** 12 locales from day one — en, ru, de, fr, es, pt, zh, ja, ko, it, pl, tr
- API returns stable **error codes**; the UI translates messages

## Non-goals (MVP)
- Matching Mailcow feature surface
- Calendar/contacts suite
- Heavy AI in the hot path
- Publishing deployment secrets or server inventory in git
- RTL locales (e.g. Arabic) in v1
