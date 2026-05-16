# Multi-stage Dockerfile for the Phaze production image.
# Build context: the repo root (fly.toml lives at the root and references
# this Dockerfile as nexus_server/Dockerfile).

# -----------------------------------------------------------------------
# 1) Build the Go binaries (nexus-server + seed-admin).
# -----------------------------------------------------------------------
FROM golang:1.26-alpine AS go-build
WORKDIR /src
# Bring in just the Go module first so the dependency download caches well.
COPY nexus_server/go.mod nexus_server/go.sum ./nexus_server/
RUN cd nexus_server && go mod download
COPY nexus_server/ ./nexus_server/
RUN cd nexus_server \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/nexus-server . \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/seed-admin ./cmd/seed-admin

# -----------------------------------------------------------------------
# 2) Build the React/Vite web client.
#    Emits a static SPA at /src/web/dist that the Go server will mount at /web.
# -----------------------------------------------------------------------
FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# -----------------------------------------------------------------------
# 3) Pull a pinned Litestream binary (SQLite-to-S3 streaming backup).
# -----------------------------------------------------------------------
FROM alpine:3.20 AS litestream-dl
ARG LITESTREAM_VERSION=0.3.13
RUN apk add --no-cache curl tar \
 && curl -sL "https://github.com/benbjohnson/litestream/releases/download/v${LITESTREAM_VERSION}/litestream-v${LITESTREAM_VERSION}-linux-amd64.tar.gz" \
    | tar -xz -C /usr/local/bin litestream

# -----------------------------------------------------------------------
# 4) Runtime image. Slim — only the binaries, the templates/public assets,
#    and the compiled web SPA.
# -----------------------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=go-build      /out/nexus-server                    /app/nexus-server
COPY --from=go-build      /out/seed-admin                      /app/seed-admin
COPY --from=litestream-dl /usr/local/bin/litestream            /usr/local/bin/litestream
COPY --from=web-build     /src/web/dist                        /app/web
COPY nexus_server/templates                                    /app/templates
COPY nexus_server/public                                       /app/public
COPY nexus_server/litestream.yml                               /app/litestream.yml
COPY nexus_server/entrypoint.sh                                /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENV PORT=8080
ENV DB_PATH=/data/nexus.db
EXPOSE 8080

CMD ["/app/entrypoint.sh"]
