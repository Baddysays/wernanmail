# Design

## Chosen baseline
**Paper Quiet** (docs/mockups/wernanmail-style-02-paper-quiet.png)

- Light surfaces, calm teal-forward accent by default
- Three columns: folders · list · reading pane
- Airy but usable density; thin icons; readable body text

## Settings (required)
1. **Font** — pick UI font (and optional reading font)
2. **Color** — accent family with several gradations (e.g. 50–900 scale via CSS variables), not one flat hex
3. **Theme** — light / dark at minimum

Implementation: CSS variables driven by settings store; no rebuild to switch theme/font/accent.

## i18n
- Every label, error, empty state, and setting is keyed
- Default locales to ship early: en, 
u (more later)
- Locale switch lives in Settings (and optionally header)

## Out of baseline (for now)
- Ink Terminal as default (keep as optional dark density later if needed)
- Harbor Trust strip can return as a Settings-toggle “health bar”, not mandatory chrome
