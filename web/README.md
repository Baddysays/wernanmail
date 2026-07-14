# Wernanmail web client

React 19 + Vite + TypeScript frontend for Wernanmail (Paper Quiet).

## Run

```bash
pnpm --dir web install
pnpm --dir web dev
```

Build:

```bash
pnpm --dir web build
```

Dev server proxies `/api` to the Go backend on `localhost:8080`.

## Stack

- React Router
- CSS Modules + CSS variables (theme, font, accent)
- i18next — 12 locales
