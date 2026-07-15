# Build webmail.
FROM node:22-alpine AS web-build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/index.html web/vite.config.ts web/tsconfig*.json ./
COPY web/public ./public
COPY web/src ./src
RUN pnpm build

# Build the admin SPA at /admin/.
FROM node:22-alpine AS admin-build
WORKDIR /app
COPY admin/package.json admin/package-lock.json ./
RUN npm ci
COPY admin/ ./
RUN npm run build

# Serve both SPAs and proxy both Go APIs.
FROM nginx:1.27-alpine
COPY web/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=web-build /app/dist /usr/share/nginx/html
COPY --from=admin-build /app/dist /usr/share/nginx/html/admin
EXPOSE 80 443
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-check-certificate -qO- https://127.0.0.1/healthz >/dev/null || exit 1