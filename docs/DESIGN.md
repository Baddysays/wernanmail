# Design

## Chosen baseline
**Paper Quiet** (`docs/mockups/wernanmail-style-02-paper-quiet.png`)

- Light surfaces, calm teal-forward accent by default
- Three columns: folders · list · reading pane
- Airy but usable density; thin icons; readable body text

### Admin console
- Overview: quiet console — `docs/mockups/admin-variant-c-quiet-console.png`
- Working screens: operator health strip — `docs/mockups/admin-variant-b-operator-strip.png`
- Alternate paper-quiet pass — `docs/mockups/admin-variant-a-paper-quiet.png`

## Motion & craft (emil-design-eng)
Apply [emilkowalski/skills · emil-design-eng](https://github.com/emilkowalski/skills):

- Press: `transform: scale(0.97)` on `:active` (~120ms, custom ease-out)
- Never `transition: all` — list only transform / color / opacity / shadow
- Hover only under `@media (hover: hover) and (pointer: fine)`
- Elevation via soft multi-layer shadows, not heavy solid borders
- Custom easings: `--ease-out: cubic-bezier(0.23, 1, 0.32, 1)`; UI under ~300ms
- Respect `prefers-reduced-motion`

## Color moods (app-wide)
Settings → **Color palette** (`mood`) drives CSS on `<html data-mood>`:

| Mood | Feel |
|------|------|
| `auto` | Time of day → harbor / reef / grove / ember / mist |
| `harbor` | Morning sea blue |
| `reef` | Midday teal (default Paper Quiet) |
| `grove` | Moss / late day |
| `ember` | Evening copper on ink |
| `mist` | Night steel |

Surfaces, accents, login hero, and mail chrome all read the same variables.

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
