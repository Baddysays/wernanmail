# Design

## Chosen baseline
**Paper Quiet** (`docs/mockups/wernanmail-style-02-paper-quiet.png`)

- Light surfaces, calm teal-forward accent by default
- Three columns: folders · list · reading pane
- Airy but usable density; thin icons; readable body text

## Settings (required)
1. **Font** — pick UI font (and optional reading font)
2. **Color** — accent family with several gradations (e.g. 50–900 scale via CSS variables), not one flat hex
3. **Theme** — light / dark at minimum
4. **Language** — one of 12 locales

Implementation: CSS variables driven by settings store; no rebuild to switch theme/font/accent.

## i18n (12 locales from day one)

| Code | Language |
|------|----------|
| `en` | English |
| `ru` | Russian |
| `de` | German |
| `fr` | French |
| `es` | Spanish |
| `pt` | Portuguese |
| `zh` | Chinese (Simplified) |
| `ja` | Japanese |
| `ko` | Korean |
| `it` | Italian |
| `pl` | Polish |
| `tr` | Turkish |

- Library: `i18next` + `react-i18next`
- Every label, error, empty state, and setting is keyed
- Dates/numbers via `Intl` (no hard-coded English formats)
- Fonts: system stacks + optional faces that cover Latin / Cyrillic / CJK
- No Arabic in v1 → no RTL complexity yet (can add later)
- Locale switch in Settings (and optionally header)

## Out of baseline (for now)
- Ink Terminal as default (keep as optional dark density later if needed)
- Harbor Trust strip can return as a Settings-toggle health bar, not mandatory chrome
