# Wernanmail policy

## Goals
1. Lightness — fit in ~700MB RAM; recommend 1GB for the full product.
2. Speed — UI and API feel instant; keyboard-driven.
3. Reliability — containerized like Mailcow (healthchecks, restart, volumes), without Mailcow resource cost.

## Phases
1. **Client** — webmail against existing IMAP/SMTP (Mailcow, etc.). Domain: wernanmail.baddysays.ru
2. **Server** — own MTA/IMAP stack, still light; integrations follow.

## Unique wedge
**Mailport** — embeddable mail surfaces (SDK / iframe) for other products.

## Non-goals (MVP)
- Matching Mailcow feature surface
- Calendar/contacts suite
- Heavy AI in the hot path
