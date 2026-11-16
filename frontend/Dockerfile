# syntax=docker/dockerfile:1

# ---- build ----
FROM oven/bun:1.3-alpine AS build
WORKDIR /app

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

COPY . .
# Same-origin API: nginx forwards /api to the backend container
ARG VITE_API_URL=/api
ENV VITE_API_URL=${VITE_API_URL}
RUN bun run build

# ---- runtime ----
FROM nginx:1.29-alpine
# The nginx image fills ${...} variables of templates/*.template from the environment at start
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
ENV TRUSTED_PROXY_CIDR=127.0.0.1
# Serve the Swagger UI (/docs); the production compose turns it off
ENV DOCS_ENABLED=true
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
